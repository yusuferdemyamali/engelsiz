package assistant

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"engelsiz/internal/download"
)

const (
	maxRuntimeFiles = 100000
	// maxRuntimeTotal caps total uncompressed runtime bytes (archive bomb guard).
	maxRuntimeTotal = 512 << 20
	// maxRuntimeFile caps a single runtime member.
	maxRuntimeFile = 256 << 20
	// maxGGUFSize is a sanity cap for a single GGUF payload install.
	maxGGUFSize = 16 << 30
)

// validRawName validates the archive member name BEFORE path.Clean, so
// normalized traversal ("a/../x") is rejected instead of accepted. Every path
// component must also satisfy the cross-platform SafeFilename rules shared
// with the download manager.
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

// validLinkTarget accepts only relative symlink targets that stay inside the
// staging root when resolved from the link's directory.
func validLinkTarget(linkName, target string) bool {
	if target == "" || strings.Contains(target, "\\") || strings.Contains(target, "\x00") {
		return false
	}
	if path.IsAbs(target) {
		return false
	}
	if len(target) >= 2 && target[1] == ':' {
		return false
	}
	dir := path.Dir(linkName)
	joined := path.Clean(path.Join(dir, target))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return false
	}
	for _, part := range strings.Split(joined, "/") {
		if part == "" {
			continue
		}
		if !download.SafeFilename(part) {
			return false
		}
	}
	return true
}

// stageGGUF streams the verified artifact handle into a fresh staging file,
// hashing the staged bytes so the manifest covers the bytes that will be
// published. The caller has already verified the artifact hash and size; this
// pass binds the staged payload to the same digest.
func stageGGUF(ctx context.Context, artifact *os.File, size int64, stagingFile string) (string, error) {
	if size <= 0 || size > maxGGUFSize {
		return "", ErrVerify
	}
	if _, err := artifact.Seek(0, io.SeekStart); err != nil {
		return "", ErrVerify
	}
	root, err := os.OpenRoot(filepath.Dir(stagingFile))
	if err != nil {
		return "", err
	}
	defer root.Close()
	name := filepath.Base(stagingFile)
	if !download.SafeFilename(name) {
		return "", ErrVerify
	}
	w, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", ErrVerify
	}
	tee := &hashReader{r: io.LimitReader(artifact, size+1), h: sha256.New()}
	written, copyErr := io.Copy(w, &cancelReader{ctx: ctx, r: tee})
	closeErr := w.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrVerify
	}
	if closeErr != nil {
		return "", ErrVerify
	}
	if written != size || tee.n != size {
		return "", ErrVerify
	}
	if fi, err := root.Lstat(name); err != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		return "", ErrVerify
	}
	sum := tee.h.Sum(nil)
	out := make([]byte, len(sum)*2)
	hex.Encode(out, sum)
	return string(out), nil
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *cancelReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// stagedMember is one validated archive entry with its root-relative name.
type stagedMember struct {
	name    string // cleaned, slash-separated, relative (after top-dir unwrap)
	raw     string // cleaned pre-unwrap archive path (tar order matching)
	isDir   bool
	isLink  bool
	target  string
	size    int64
	file    *zip.File // zip only
	tarType byte      // tar only
}

// commonTop returns the single top-level directory shared by every member, or
// "" when members span multiple tops.
func commonTop(names []string) string {
	top := ""
	single := true
	for _, name := range names {
		head, _, _ := strings.Cut(name, "/")
		if top == "" {
			top = head
		} else if head != top {
			single = false
			break
		}
	}
	if single && top != "" {
		return top + "/"
	}
	return ""
}

func unwrapName(cleaned, strip string) (string, bool) {
	if strip == "" {
		return cleaned, true
	}
	if cleaned == strings.TrimSuffix(strip, "/") {
		return "", false // the top-level directory entry itself
	}
	name := path.Clean(strings.TrimPrefix(cleaned, strip))
	if !validCleanName(name) {
		return "", false
	}
	return name, true
}

// planZipMembers validates every zip member and unwraps a single top-level
// directory. Safe symlinks are allowed and validated lexically here, then
// re-checked after creation.
func planZipMembers(z *zip.Reader, pin RuntimeAcquisition) ([]stagedMember, error) {
	if len(z.File) == 0 || len(z.File) > maxRuntimeFiles {
		return nil, ErrVerify
	}
	cleaned := make([]string, 0, len(z.File))
	seen := make(map[string]struct{}, len(z.File))
	var total uint64
	for _, f := range z.File {
		if !validRawName(f.Name) {
			return nil, ErrVerify
		}
		clean := path.Clean(f.Name)
		if !validCleanName(clean) {
			return nil, ErrVerify
		}
		mode := f.Mode()
		isLink := mode&os.ModeSymlink != 0
		isDir := f.FileInfo().IsDir()
		if !isLink && !isDir && !mode.IsRegular() {
			return nil, ErrVerify
		}
		if isLink {
			if f.UncompressedSize64 > 4096 {
				return nil, ErrVerify
			}
		} else if !isDir {
			if f.UncompressedSize64 > maxRuntimeFile {
				return nil, ErrVerify
			}
			total += f.UncompressedSize64
			if total > maxRuntimeTotal {
				return nil, ErrVerify
			}
		}
		lower := strings.ToLower(clean)
		if _, dup := seen[lower]; dup {
			return nil, ErrVerify // case collision: unsafe on Windows
		}
		seen[lower] = struct{}{}
		cleaned = append(cleaned, clean)
	}
	strip := commonTop(cleaned)
	out := make([]stagedMember, 0, len(z.File))
	for i, f := range z.File {
		name, keep := unwrapName(cleaned[i], strip)
		if !keep {
			continue
		}
		mode := f.Mode()
		m := stagedMember{name: name, file: f}
		switch {
		case mode&os.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return nil, ErrVerify
			}
			target, err := io.ReadAll(io.LimitReader(rc, 4097))
			rc.Close()
			if err != nil {
				return nil, ErrVerify
			}
			m.isLink = true
			m.target = string(target)
			if !validLinkTarget(name, m.target) {
				return nil, ErrVerify
			}
		case f.FileInfo().IsDir():
			m.isDir = true
		case mode.IsRegular():
			m.size = int64(f.UncompressedSize64)
		default:
			return nil, ErrVerify
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, ErrVerify
	}
	return out, nil
}

// extractZipMembers writes validated members under stagingRoot through a
// confined os.Root. Symlinks are created last and re-validated after creation.
func extractZipMembers(ctx context.Context, stagingRoot string, members []stagedMember) error {
	root, err := os.OpenRoot(stagingRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	var links []stagedMember
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
		if m.isLink {
			links = append(links, m)
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
		w, err := root.OpenFile(m.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			rc.Close()
			return ErrVerify
		}
		written, copyErr := io.Copy(w, io.LimitReader(&cancelReader{ctx: ctx, r: rc}, maxRuntimeFile+1))
		closeErr := w.Close()
		rc.Close()
		if copyErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrVerify
		}
		if closeErr != nil {
			return ErrVerify
		}
		if written != m.size {
			return ErrVerify
		}
		if fi, err := root.Lstat(m.name); err != nil || !fi.Mode().IsRegular() || fi.Size() != written {
			return ErrVerify
		}
	}
	return createSafeLinks(stagingRoot, links)
}

// scanTarHeaders reads all tar headers (validating names, types and sizes)
// and returns the top-dir strip plus per-entry metadata in order.
func scanTarHeaders(r *tar.Reader) ([]stagedMember, string, error) {
	var members []stagedMember
	var cleaned []string
	seen := map[string]struct{}{}
	var total uint64
	for {
		hdr, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", ErrVerify
		}
		if !validRawName(hdr.Name) {
			return nil, "", ErrVerify
		}
		clean := path.Clean(hdr.Name)
		if !validCleanName(clean) {
			return nil, "", ErrVerify
		}
		lower := strings.ToLower(clean)
		if _, dup := seen[lower]; dup {
			return nil, "", ErrVerify
		}
		seen[lower] = struct{}{}
		var m stagedMember
		switch hdr.Typeflag {
		case tar.TypeDir:
			m = stagedMember{name: clean, raw: clean, isDir: true}
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size < 0 || hdr.Size > maxRuntimeFile {
				return nil, "", ErrVerify
			}
			total += uint64(hdr.Size)
			if total > maxRuntimeTotal {
				return nil, "", ErrVerify
			}
			m = stagedMember{name: clean, raw: clean, size: hdr.Size, tarType: hdr.Typeflag}
		case tar.TypeSymlink:
			if !validLinkTarget(clean, hdr.Linkname) {
				return nil, "", ErrVerify
			}
			m = stagedMember{name: clean, raw: clean, isLink: true, target: hdr.Linkname, tarType: hdr.Typeflag}
		default:
			return nil, "", ErrVerify // hardlinks, devices, fifos rejected
		}
		members = append(members, m)
		cleaned = append(cleaned, clean)
		// Consume the file body to advance to the next header.
		if m.tarType == tar.TypeReg || m.tarType == tar.TypeRegA {
			if _, err := io.Copy(io.Discard, io.LimitReader(r, maxRuntimeFile+1)); err != nil {
				return nil, "", ErrVerify
			}
		}
		if len(members) > maxRuntimeFiles {
			return nil, "", ErrVerify
		}
	}
	if len(members) == 0 {
		return nil, "", ErrVerify
	}
	strip := commonTop(cleaned)
	droppedTop := strings.TrimSuffix(strip, "/")
	out := members[:0]
	for _, m := range members {
		name, keep := unwrapName(m.name, strip)
		if !keep {
			continue
		}
		m.name = name
		if m.isLink && !validLinkTarget(name, m.target) {
			return nil, "", ErrVerify
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, "", ErrVerify
	}
	return out, droppedTop, nil
}

// extractTarGz validates then extracts a gzip-compressed tar archive. The
// artifact is streamed twice from disk (scan, then extract) so every header
// is validated before its bytes are written.
func extractTarGz(ctx context.Context, artifact *os.File, size int64, stagingRoot string) error {
	if _, err := artifact.Seek(0, io.SeekStart); err != nil {
		return ErrVerify
	}
	gr, err := gzip.NewReader(io.LimitReader(&cancelReader{ctx: ctx, r: artifact}, size+1))
	if err != nil {
		return ErrVerify
	}
	members, droppedTop, err := scanTarHeaders(tar.NewReader(gr))
	gr.Close()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var problem *download.Error
		if errors.As(err, &problem) {
			return problem
		}
		return ErrVerify
	}
	if _, err := artifact.Seek(0, io.SeekStart); err != nil {
		return ErrVerify
	}
	gr, err = gzip.NewReader(io.LimitReader(&cancelReader{ctx: ctx, r: artifact}, size+1))
	if err != nil {
		return ErrVerify
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	root, err := os.OpenRoot(stagingRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	var links []stagedMember
	want := make([]stagedMember, len(members))
	copy(want, members)
	for len(want) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err != nil {
			return ErrVerify
		}
		clean := path.Clean(hdr.Name)
		if droppedTop != "" && clean == droppedTop {
			// The unwrapped top-level directory entry itself.
			continue
		}
		// Headers arrive in scan order; the cleaned name must match the plan.
		m := want[0]
		if path.Clean(hdr.Name) != m.raw {
			return ErrVerify
		}
		want = want[1:]
		parent := path.Dir(m.name)
		if parent != "." {
			if err := mkdirAllRoot(root, parent); err != nil {
				return err
			}
		}
		switch {
		case m.isDir:
			if err := mkdirAllRoot(root, m.name); err != nil {
				return err
			}
		case m.isLink:
			links = append(links, m)
			// No body for symlinks; continue to next header.
		default:
			if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
				return ErrVerify
			}
			if hdr.Size != m.size {
				return ErrVerify
			}
			if _, err := root.Lstat(m.name); err == nil {
				return ErrVerify
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			w, err := root.OpenFile(m.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				return ErrVerify
			}
			written, copyErr := io.Copy(w, io.LimitReader(&cancelReader{ctx: ctx, r: tr}, maxRuntimeFile+1))
			closeErr := w.Close()
			if copyErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ErrVerify
			}
			if closeErr != nil {
				return ErrVerify
			}
			if written != m.size {
				return ErrVerify
			}
			if fi, err := root.Lstat(m.name); err != nil || !fi.Mode().IsRegular() || fi.Size() != written {
				return ErrVerify
			}
		}
	}
	if len(want) != 0 {
		return ErrVerify
	}
	return createSafeLinks(stagingRoot, links)
}

// createSafeLinks creates validated symlinks inside stagingRoot and
// re-checks that every link resolves inside the staging tree. Links are
// created in two phases so safe forward/chained references (a link whose
// target is another link listed later in the archive, as shipped by the
// real llama.cpp tarballs) do not fail: phase 1 creates every entry after
// lexical validation, phase 2 resolves every chain once all entries exist.
func createSafeLinks(stagingRoot string, links []stagedMember) error {
	for _, m := range links {
		if !m.isLink || !validCleanName(m.name) || !validLinkTarget(m.name, m.target) {
			return ErrVerify
		}
		dst := filepath.Join(stagingRoot, filepath.FromSlash(m.name))
		if _, err := os.Lstat(dst); err == nil {
			return ErrVerify
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Symlink(m.target, dst); err != nil {
			return ErrVerify
		}
	}
	for _, m := range links {
		dst := filepath.Join(stagingRoot, filepath.FromSlash(m.name))
		resolved, err := filepath.EvalSymlinks(dst)
		if err != nil {
			return ErrVerify
		}
		if !insideRoot(stagingRoot, resolved) {
			return ErrVerify
		}
		got, err := os.Readlink(dst)
		if err != nil || got != m.target {
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

// serverBinaryNames are the accepted llama.cpp server entry points inside a
// verified runtime archive.
func serverBinaryNames(goos string) []string {
	if goos == "windows" {
		return []string{"llama-server.exe"}
	}
	return []string{"llama-server"}
}

// findServerBinary locates the server entry point in the staged tree by
// manifest entry (regular file with an accepted basename).
func findServerBinary(files []receiptFile, goos string) string {
	want := map[string]bool{}
	for _, n := range serverBinaryNames(goos) {
		want[strings.ToLower(n)] = true
	}
	for _, f := range files {
		if f.Link != "" {
			continue
		}
		if want[strings.ToLower(path.Base(f.Name))] {
			return f.Name
		}
	}
	return ""
}

// stageName makes an unpredictable staging sibling for atomic publish.
func stageName(base string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return ".staging-" + base + "-" + hex.EncodeToString(nonce[:])
}

// brokenName stages a corrupt model directory aside for explicit repair. It
// renames within the same parent (no deletion, no cross-directory moves).
func brokenName(base string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return base + ".broken-" + hex.EncodeToString(nonce[:])
}
