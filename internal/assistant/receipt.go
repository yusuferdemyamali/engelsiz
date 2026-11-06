package assistant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"engelsiz/internal/download"
)

const (
	// receiptName is written into staging before atomic publish and
	// re-validated (binding only) on every startup. Payload bytes are hashed
	// during install (artifact stream plus staged copy); they are never
	// re-hashed on boot or List.
	receiptName = "engelsiz-receipt.json"
	// modelFileName is the single GGUF payload inside a published model dir.
	modelFileName = "model.gguf"
)

// receiptFile is one installed payload entry. Regular files carry size+hash;
// safe symlinks (runtime archives only) carry their validated target.
type receiptFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}

// receipt binds an installed model or runtime directory to the verified pin
// it was installed from.
type receipt struct {
	ID      string        `json:"id"`
	Version string        `json:"version"`
	SHA256  string        `json:"sha256"`
	Binary  string        `json:"binary,omitempty"`
	Files   []receiptFile `json:"files"`
}

// hashReader tees a SHA-256 over a stream while counting bytes.
type hashReader struct {
	r io.Reader
	h hash.Hash
	n int64
}

func (t *hashReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	t.n += int64(n)
	if n > 0 {
		_, _ = t.h.Write(p[:n])
	}
	return n, err
}

const (
	// maxReceiptBytes caps the install receipt read: a tampered or corrupt
	// receipt can never force an unbounded allocation.
	maxReceiptBytes = 1 << 20
	// maxReceiptFiles caps manifest entries accepted from a receipt.
	maxReceiptFiles = 10000
)

// writeModelReceipt records the staged model tree without re-hashing the
// payload: the staged file digest is passed in from the install stream that
// already verified it. The tree walk below only checks names and kinds.
func writeModelReceipt(stagingRoot, id string, pin Acquisition, payloadSHA string) error {
	var names []string
	err := filepath.WalkDir(stagingRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == stagingRoot {
			return nil
		}
		rel, err := filepath.Rel(stagingRoot, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !validCleanName(name) {
			return ErrVerify
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrVerify
		}
		names = append(names, name)
		return nil
	})
	if err != nil {
		var problem *download.Error
		if errors.As(err, &problem) {
			return problem
		}
		return ErrVerify
	}
	if len(names) != 1 || names[0] != modelFileName {
		return ErrVerify
	}
	if payloadSHA != strings.ToLower(pin.ChecksumValue) {
		return ErrVerify
	}
	raw, err := json.Marshal(receipt{ID: id, Version: pin.Version, SHA256: payloadSHA, Files: []receiptFile{{Name: modelFileName, Size: pin.ExpectedSize, SHA256: payloadSHA}}})
	if err != nil {
		return ErrVerify
	}
	return os.WriteFile(filepath.Join(stagingRoot, receiptName), raw, 0o600)
}

// writeRuntimeReceipt records the staged runtime tree including the resolved
// server binary relative path and safe symlink targets.
func writeRuntimeReceipt(stagingRoot string, pin RuntimeAcquisition, binaryRel string, payloadSHA string) error {
	files, err := manifestFiles(stagingRoot, true)
	if err != nil {
		return err
	}
	if len(files) == 0 || binaryRel == "" {
		return ErrVerify
	}
	found := false
	for _, f := range files {
		if f.Name == binaryRel && f.Link == "" {
			found = true
		}
	}
	if !found {
		return ErrVerify
	}
	raw, err := json.Marshal(receipt{ID: RuntimePackageID, Version: pin.Version, SHA256: payloadSHA, Binary: binaryRel, Files: files})
	if err != nil {
		return ErrVerify
	}
	return os.WriteFile(filepath.Join(stagingRoot, receiptName), raw, 0o600)
}

func manifestFiles(stagingRoot string, allowLinks bool) ([]receiptFile, error) {
	var files []receiptFile
	err := filepath.WalkDir(stagingRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == stagingRoot {
			return nil
		}
		rel, err := filepath.Rel(stagingRoot, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !validCleanName(name) {
			return ErrVerify
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return nil
		case info.Mode()&os.ModeSymlink != 0:
			if !allowLinks {
				return ErrVerify
			}
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if !validLinkTarget(name, target) {
				return ErrVerify
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				return ErrVerify
			}
			if !insideRoot(stagingRoot, resolved) {
				return ErrVerify
			}
			files = append(files, receiptFile{Name: name, Link: target})
			return nil
		case info.Mode().IsRegular():
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			h := sha256.New()
			n, err := io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return ErrVerify
			}
			fi, err := os.Lstat(p)
			if err != nil || !fi.Mode().IsRegular() || fi.Size() != n {
				return ErrVerify
			}
			files = append(files, receiptFile{Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
			return nil
		default:
			return ErrVerify
		}
	})
	if err != nil {
		var problem *download.Error
		if errors.As(err, &problem) {
			return nil, problem
		}
		return nil, ErrVerify
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func readReceipt(root string) (receipt, error) {
	f, err := os.Open(filepath.Join(root, receiptName))
	if err != nil {
		return receipt{}, err
	}
	defer f.Close()
	// Bounded: a corrupt or hostile receipt cannot drive allocation.
	raw, err := io.ReadAll(io.LimitReader(f, maxReceiptBytes+1))
	if err != nil {
		return receipt{}, ErrVerify
	}
	if len(raw) > maxReceiptBytes {
		return receipt{}, ErrVerify
	}
	var r receipt
	if json.Unmarshal(raw, &r) != nil {
		return receipt{}, ErrVerify
	}
	if len(r.Files) > maxReceiptFiles {
		return receipt{}, ErrVerify
	}
	return r, nil
}

// checkModelBinding verifies receipt identity against the current pin without
// touching payload bytes: cheap and safe for List polling and startup. The
// recorded payload hash must equal the pinned artifact hash, not merely be
// well-formed hex: binding to any other digest is rejected.
func checkModelBinding(r receipt, def Definition, payloadSize int64) error {
	pin := def.Acquisition
	want := strings.ToLower(pin.ChecksumValue)
	if r.ID != def.ID || r.Version != pin.Version || r.SHA256 != want {
		return ErrVerify
	}
	if len(r.Files) != 1 || r.Files[0].Name != modelFileName || r.Files[0].Link != "" {
		return ErrVerify
	}
	f := r.Files[0]
	if f.Size != pin.ExpectedSize || f.Size != payloadSize || f.Size <= 0 {
		return ErrVerify
	}
	if f.SHA256 != want {
		return ErrVerify
	}
	return nil
}

// checkRuntimeBinding verifies receipt identity against the current runtime
// pin without hashing payload bytes.
func checkRuntimeBinding(r receipt, pin RuntimeAcquisition) error {
	if r.ID != RuntimePackageID || r.Version != pin.Version || r.SHA256 != strings.ToLower(pin.ChecksumValue) || len(r.Files) == 0 {
		return ErrVerify
	}
	if len(r.Files) > maxReceiptFiles {
		return ErrVerify
	}
	if r.Binary == "" || !validCleanName(r.Binary) {
		return ErrVerify
	}
	found := false
	for _, f := range r.Files {
		if !validCleanName(f.Name) {
			return ErrVerify
		}
		if f.Link != "" {
			if !validLinkTarget(f.Name, f.Link) {
				return ErrVerify
			}
			continue
		}
		if f.Size <= 0 || f.Size > maxRuntimeFile {
			return ErrVerify
		}
		raw, err := hex.DecodeString(f.SHA256)
		if err != nil || len(raw) != sha256.Size {
			return ErrVerify
		}
		if f.Name == r.Binary {
			found = true
		}
	}
	if !found {
		return ErrVerify
	}
	return nil
}

// quickValidModel performs the cheap installed-model checks used by List and
// gating: receipt bound to the pin plus the payload present as a regular file
// with the exact pinned size. No payload hashing.
func (s *Service) quickValidModel(def Definition) bool {
	dir, err := s.models.Lstat(def.ID)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return false
	}
	fi, err := s.models.Lstat(def.ID + "/" + modelFileName)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != def.Acquisition.ExpectedSize {
		return false
	}
	r, err := readReceipt(filepath.Join(s.modelsDir, def.ID))
	if err != nil {
		return false
	}
	return checkModelBinding(r, def, fi.Size()) == nil
}

// quickValidRuntime performs the cheap installed-runtime checks: receipt bound
// to the pin plus every manifest entry present with matching size/target.
func (s *Service) quickValidRuntime(pin RuntimeAcquisition) bool {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	dir, err := rt.Lstat(".")
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return false
	}
	r, err := readReceipt(s.runtimeDir)
	if err != nil {
		return false
	}
	if checkRuntimeBinding(r, pin) != nil {
		return false
	}
	for _, f := range r.Files {
		fi, err := rt.Lstat(f.Name)
		if err != nil {
			return false
		}
		if f.Link != "" {
			if fi.Mode()&os.ModeSymlink == 0 {
				return false
			}
			target, err := os.Readlink(filepath.Join(s.runtimeDir, filepath.FromSlash(f.Name)))
			if err != nil || target != f.Link {
				return false
			}
			continue
		}
		if !fi.Mode().IsRegular() || fi.Size() != f.Size {
			return false
		}
	}
	bin, err := rt.Lstat(r.Binary)
	if err != nil || !bin.Mode().IsRegular() {
		return false
	}
	return true
}

func insideRoot(root, p string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absP)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
