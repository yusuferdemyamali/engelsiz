package translation

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"engelsiz/internal/download"
)

const (
	maxExtractFiles = 10000
	// maxExtractTotal caps total uncompressed bytes (zip bomb guard).
	maxExtractTotal = 1 << 30
	// maxExtractFile caps a single member.
	maxExtractFile = 512 << 20
)

// requiredFiles are the Argos payload members every installed model must
// carry, relative to the model root (after top-dir unwrap).
var requiredFiles = []string{"metadata.json", "model/model.bin", "sentencepiece.model"}

// receiptName is the install receipt written beside the payload at publish.
const receiptName = "engelsiz-receipt.json"

// validRawName validates the archive member name BEFORE path.Clean, so
// normalized traversal ("a/../x") is rejected instead of accepted. Every path
// component must also satisfy the cross-platform SafeFilename rules shared
// with the download manager (no colons, no Windows reserved stems, no
// trailing dots/spaces, no control characters).
func validRawName(raw string) bool {
	if raw == "" || strings.Contains(raw, "\\") || strings.Contains(raw, "\x00") {
		return false
	}
	if len(raw) >= 2 && raw[1] == ':' && ((raw[0] >= 'A' && raw[0] <= 'Z') || (raw[0] >= 'a' && raw[0] <= 'z')) {
		return false // Windows drive prefix
	}
	if path.IsAbs(raw) {
		return false
	}
	// A single trailing slash only marks a directory entry.
	trimmed := strings.TrimSuffix(raw, "/")
	for _, part := range strings.Split(trimmed, "/") {
		if !download.SafeFilename(part) {
			return false
		}
	}
	return true
}

// validCleanName re-validates after Clean as defense in depth.
func validCleanName(clean string) bool {
	if clean == "" || clean == "." {
		return false
	}
	for _, part := range strings.Split(clean, "/") {
		if !download.SafeFilename(part) {
			return false
		}
	}
	return true
}

// memberMode rejects symlinks and special files; only dirs and regulars pass.
func memberMode(f *zip.File) (isDir bool, ok bool) {
	mode := f.Mode()
	if mode&os.ModeSymlink != 0 {
		return false, false
	}
	if f.FileInfo().IsDir() {
		return true, true
	}
	if mode.IsRegular() {
		return false, true
	}
	return false, false
}

type stagedMember struct {
	name  string // cleaned, slash-separated, relative
	isDir bool
	file  *zip.File
}

// planMembers validates every archive member and unwraps a single top-level
// directory (the .argosmodel layout). It returns members with root-relative
// target names.
func planMembers(z *zip.Reader) ([]stagedMember, error) {
	if len(z.File) == 0 || len(z.File) > maxExtractFiles {
		return nil, ErrVerify
	}
	cleaned := make([]string, 0, len(z.File))
	seen := make(map[string]struct{}, len(z.File))
	var total uint64
	top := ""
	singleTop := true
	for _, f := range z.File {
		if !validRawName(f.Name) {
			return nil, ErrVerify
		}
		clean := path.Clean(f.Name)
		if !validCleanName(clean) {
			return nil, ErrVerify
		}
		if _, ok := memberMode(f); !ok {
			return nil, ErrVerify
		}
		lower := strings.ToLower(clean)
		if _, dup := seen[lower]; dup {
			return nil, ErrVerify // case collision: unsafe on Windows
		}
		seen[lower] = struct{}{}
		if f.UncompressedSize64 > maxExtractFile {
			return nil, ErrVerify
		}
		total += f.UncompressedSize64
		if total > maxExtractTotal {
			return nil, ErrVerify
		}
		cleaned = append(cleaned, clean)
		head, _, _ := strings.Cut(clean, "/")
		if top == "" {
			top = head
		} else if head != top {
			singleTop = false
		}
	}
	strip := ""
	if singleTop && top != "" {
		// Only unwrap when the top component is a directory shared by all
		// members (file or implied).
		strip = top + "/"
	}
	out := make([]stagedMember, 0, len(z.File))
	for i, f := range z.File {
		name := cleaned[i]
		if strip != "" {
			if name == top {
				continue // the top-level directory entry itself
			}
			name = path.Clean(strings.TrimPrefix(name, strip))
			if !validCleanName(name) {
				return nil, ErrVerify
			}
		}
		isDir, _ := memberMode(f)
		out = append(out, stagedMember{name: name, isDir: isDir, file: f})
	}
	if len(out) == 0 {
		return nil, ErrVerify
	}
	return out, nil
}

// extractStaging writes validated members under stagingRoot through a confined
// os.Root. Every write is bounded and must equal the ZIP-declared member size
// exactly; pre-existing names are never replaced. Cancellation aborts promptly
// between members.
func extractStaging(ctx context.Context, stagingRoot string, members []stagedMember) error {
	root, err := os.OpenRoot(stagingRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, m := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validCleanName(m.name) {
			return ErrVerify
		}
		parent := path.Dir(m.name)
		if parent != "." {
			if err := mkdirAllRoot(root, parent); err != nil {
				return err
			}
		}
		if m.isDir {
			if err := mkdirAllRoot(root, m.name); err != nil {
				return err
			}
			continue
		}
		if _, err := root.Lstat(m.name); err == nil {
			return ErrVerify
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		rc, err := m.file.Open()
		if err != nil {
			return ErrVerify
		}
		w, err := root.OpenFile(m.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			rc.Close()
			return ErrVerify
		}
		written, copyErr := io.Copy(w, io.LimitReader(rc, maxExtractFile+1))
		closeErr := w.Close()
		rc.Close()
		if copyErr != nil {
			return ErrVerify
		}
		if closeErr != nil {
			return ErrVerify
		}
		// The stored bytes must equal the declared member size exactly:
		// truncated or overlong members are rejected.
		if written != int64(m.file.UncompressedSize64) {
			return ErrVerify
		}
		if fi, err := root.Lstat(m.name); err != nil || !fi.Mode().IsRegular() || fi.Size() != written {
			return ErrVerify
		}
	}
	return nil
}

func mkdirAllRoot(root *os.Root, dir string) error {
	if !validCleanName(dir) {
		return ErrVerify
	}
	parts := strings.Split(dir, "/")
	cur := ""
	for _, part := range parts {
		if cur == "" {
			cur = part
		} else {
			cur += "/" + part
		}
		if err := root.Mkdir(cur, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return ErrVerify
		}
		if fi, err := root.Lstat(cur); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return ErrVerify
		}
	}
	return nil
}

// stagedMetadata reads and validates the extracted metadata.json: direction
// must match and package_version must equal the pinned version.
func stagedMetadata(stagingRoot, wantFrom, wantTo, wantVersion string) error {
	raw, err := os.ReadFile(filepath.Join(stagingRoot, "metadata.json"))
	if err != nil {
		return ErrVerify
	}
	var meta struct {
		FromCode       string `json:"from_code"`
		ToCode         string `json:"to_code"`
		PackageVersion string `json:"package_version"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return ErrVerify
	}
	if meta.FromCode != wantFrom || meta.ToCode != wantTo || meta.PackageVersion != wantVersion {
		return ErrVerify
	}
	return nil
}

// checkStagedModel requires the real Argos payload: metadata.json with pinned
// direction/version plus model/model.bin and sentencepiece.model.
func checkStagedModel(stagingRoot, wantFrom, wantTo, wantVersion string) error {
	if err := stagedMetadata(stagingRoot, wantFrom, wantTo, wantVersion); err != nil {
		return err
	}
	for _, name := range requiredFiles {
		fi, err := os.Lstat(filepath.Join(stagingRoot, filepath.FromSlash(name)))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
			return ErrVerify
		}
	}
	return nil
}

// stageName makes an unpredictable staging sibling for atomic publish.
func stageName(modelID string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return ".staging-" + modelID + "-" + hex.EncodeToString(nonce[:])
}

// brokenName stages a corrupt model directory aside for explicit repair. It
// renames within the same parent (no deletion, no cross-directory moves).
func brokenName(modelID string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return modelID + ".broken-" + hex.EncodeToString(nonce[:])
}
