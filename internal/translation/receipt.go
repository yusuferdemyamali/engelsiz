package translation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"engelsiz/internal/download"
)

// receiptFile is one installed payload file with its verified size and hash.
type receiptFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// receipt binds an installed model directory to the verified pin it was
// installed from, with a per-file size/hash manifest generated from the
// verified extraction. It is written into staging before atomic publish and
// re-validated on every startup; GET performs only cheap presence checks.
type receipt struct {
	ID      string        `json:"id"`
	Version string        `json:"version"`
	SHA256  string        `json:"sha256"`
	Files   []receiptFile `json:"files"`
}

// writeReceipt walks the staged tree and records every regular file. The
// receipt itself is written after the walk so it never covers itself.
func writeReceipt(stagingRoot string, def Definition, pin Acquisition) error {
	var files []receiptFile
	err := filepath.WalkDir(stagingRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			if d.Name() == receiptName {
				return ErrVerify
			}
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
	})
	if err != nil {
		var problem *download.Error
		if errors.As(err, &problem) {
			return problem
		}
		return ErrVerify
	}
	if len(files) == 0 {
		return ErrVerify
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	raw, err := json.Marshal(receipt{ID: def.ID, Version: pin.Version, SHA256: strings.ToLower(pin.ChecksumValue), Files: files})
	if err != nil {
		return ErrVerify
	}
	return os.WriteFile(filepath.Join(stagingRoot, receiptName), raw, 0o600)
}

// readReceipt parses the installed receipt without hashing payload files.
func readReceipt(modelRoot string) (receipt, error) {
	raw, err := os.ReadFile(filepath.Join(modelRoot, receiptName))
	if err != nil {
		return receipt{}, err
	}
	var r receipt
	if json.Unmarshal(raw, &r) != nil {
		return receipt{}, ErrVerify
	}
	return r, nil
}

// checkReceiptBinding verifies the receipt identity against the current pin
// without touching payload files (cheap; suitable for GET polling).
func checkReceiptBinding(r receipt, def Definition, pin Acquisition) error {
	if r.ID != def.ID || r.Version != pin.Version || r.SHA256 != strings.ToLower(pin.ChecksumValue) || len(r.Files) == 0 {
		return ErrVerify
	}
	for _, f := range r.Files {
		if !validCleanName(f.Name) || f.Size <= 0 || f.Size > maxExtractFile {
			return ErrVerify
		}
		raw, err := hex.DecodeString(f.SHA256)
		if err != nil || len(raw) != sha256.Size {
			return ErrVerify
		}
	}
	return nil
}

// validateReceipt fully validates an installed model directory: receipt
// binding plus required files plus per-file size/hash verification. It
// performs no networking.
func validateReceipt(modelRoot string, def Definition, pin Acquisition) error {
	r, err := readReceipt(modelRoot)
	if err != nil {
		return ErrVerify
	}
	if err := checkReceiptBinding(r, def, pin); err != nil {
		return err
	}
	seen := make(map[string]receiptFile, len(r.Files))
	for _, f := range r.Files {
		seen[f.Name] = f
	}
	for _, name := range requiredFiles {
		if _, ok := seen[name]; !ok {
			return ErrVerify
		}
	}
	for _, f := range r.Files {
		p := filepath.Join(modelRoot, filepath.FromSlash(f.Name))
		fh, err := os.Open(p)
		if err != nil {
			return ErrVerify
		}
		h := sha256.New()
		n, err := io.Copy(h, fh)
		closeErr := fh.Close()
		if err != nil || closeErr != nil || n != f.Size {
			return ErrVerify
		}
		if hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return ErrVerify
		}
		if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() || fi.Size() != f.Size {
			return ErrVerify
		}
	}
	return nil
}

// quickValid performs cheap installed-model checks for GET polling and
// translate gating: required files present and receipt bound to the pin.
// Payload hashing happens only at install and startup.
func (s *Service) quickValid(def Definition) bool {
	for _, name := range requiredFiles {
		fi, err := s.models.Lstat(def.ID + "/" + name)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
			return false
		}
	}
	dir, err := s.models.Lstat(def.ID)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return false
	}
	r, err := readReceipt(filepath.Join(s.dir, def.ID))
	if err != nil {
		return false
	}
	return checkReceiptBinding(r, def, def.Acquisition) == nil
}
