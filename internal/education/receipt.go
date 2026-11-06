package education

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
)

// receiptFile is the single installed payload file with its verified size and
// hash. Exactly one entry is ever valid: the pinned resource file itself.
type receiptFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// receipt binds an installed content directory to the verified pin it was
// installed from. It is written into staging before atomic publish and
// re-validated on every startup; GET performs only cheap presence checks.
type receipt struct {
	ID      string        `json:"id"`
	Version string        `json:"version"`
	SHA256  string        `json:"sha256"`
	Files   []receiptFile `json:"files"`
}

// receiptName is the install receipt written beside the payload at publish.
const receiptName = "engelsiz-receipt.json"

// writeReceipt records the pinned payload in the staging root. The file entry
// is the trusted pin, never a self-report: validation always re-hashes the
// payload against the pin.
func writeReceipt(st *os.Root, def Resource) error {
	entry := receiptFile{Name: def.Filename, Size: *def.SizeBytes, SHA256: strings_Lower(def.SHA256)}
	raw, err := json.Marshal(receipt{ID: def.ID, Version: def.Version, SHA256: strings_Lower(def.SHA256), Files: []receiptFile{entry}})
	if err != nil {
		return ErrVerify
	}
	w, err := st.OpenFile(receiptName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrVerify
	}
	if _, err := w.Write(raw); err != nil {
		w.Close()
		return ErrVerify
	}
	if err := w.Close(); err != nil {
		return ErrVerify
	}
	return nil
}

// readReceipt parses the installed receipt through the confined root. The
// receipt itself must be a regular file: a symlinked receipt is rejected
// without following it.
func readReceipt(root *os.Root, dir string) (receipt, error) {
	name := dir + "/" + receiptName
	fi, err := root.Lstat(name)
	if err != nil || !fi.Mode().IsRegular() {
		return receipt{}, ErrVerify
	}
	raw, err := root.ReadFile(name)
	if err != nil {
		return receipt{}, ErrVerify
	}
	var r receipt
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return receipt{}, ErrVerify
	}
	return r, nil
}

// checkReceiptBinding verifies the receipt identity against the current pin:
// exactly one payload entry naming the pinned file with the pinned size and
// hash. No payload bytes are touched (cheap; suitable for GET polling).
func checkReceiptBinding(r receipt, def Resource) error {
	if r.ID != def.ID || r.Version != def.Version || r.SHA256 != strings_Lower(def.SHA256) {
		return ErrVerify
	}
	if len(r.Files) != 1 {
		return ErrVerify
	}
	f := r.Files[0]
	if f.Name != def.Filename || f.Size != *def.SizeBytes || f.SHA256 != strings_Lower(def.SHA256) {
		return ErrVerify
	}
	if _, err := hex.DecodeString(f.SHA256); err != nil {
		return ErrVerify
	}
	return nil
}

// validatePayload fully validates an installed directory through the confined
// root: directory identity, receipt binding, then the actual payload file
// hashed against the trusted pin (never against self-reported values).
// Symlinked directories, files or receipts are rejected without following.
func validatePayload(root *os.Root, def Resource) error {
	dir, err := root.Lstat(def.ID)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return ErrVerify
	}
	r, err := readReceipt(root, def.ID)
	if err != nil {
		return err
	}
	if err := checkReceiptBinding(r, def); err != nil {
		return err
	}
	name := def.ID + "/" + def.Filename
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != *def.SizeBytes {
		return ErrVerify
	}
	fh, err := root.Open(name)
	if err != nil {
		return ErrVerify
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(before, fi) || fi.Size() != *def.SizeBytes {
		return ErrVerify
	}
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return ErrVerify
	}
	if hex.EncodeToString(h.Sum(nil)) != strings_Lower(def.SHA256) {
		return ErrVerify
	}
	after, err := root.Lstat(name)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(fi, after) || after.Size() != *def.SizeBytes {
		return ErrVerify
	}
	return nil
}

// quickValid performs cheap installed-file checks for GET polling and open
// gating through the confined root: directory and payload presence with exact
// pinned size plus receipt binding. Payload hashing happens only at install
// and startup. A truncated file fails the exact-size check.
func (s *Service) quickValid(def Resource) bool {
	if def.Format == FormatZIM || !def.VerifiedPin() {
		return false
	}
	dir, err := s.files.Lstat(def.ID)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return false
	}
	fi, err := s.files.Lstat(def.ID + "/" + def.Filename)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != *def.SizeBytes {
		return false
	}
	r, err := readReceipt(s.files, def.ID)
	if err != nil {
		return false
	}
	return checkReceiptBinding(r, def) == nil
}

func strings_Lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
