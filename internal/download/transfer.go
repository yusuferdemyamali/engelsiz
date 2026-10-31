package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var rangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func contentRange(s string) (start, end, total int64, ok bool) {
	match := rangePattern.FindStringSubmatch(s)
	if match == nil {
		return
	}
	start, e1 := strconv.ParseInt(match[1], 10, 64)
	end, e2 := strconv.ParseInt(match[2], 10, 64)
	total, e3 := strconv.ParseInt(match[3], 10, 64)
	return start, end, total, e1 == nil && e2 == nil && e3 == nil && start >= 0 && end >= start && total > end
}
func (m *Manager) transfer(ctx context.Context, r *record) error {
	actual, e := m.stat(*r)
	if e != nil {
		return ErrStorage
	}
	r.DownloadedBytes = actual
	offset := actual
	v := validator(r)
	if v == "" {
		offset = 0
	}
	recovery := actual > 0
	var restartTotal *int64
	var restartETag, restartModified string
	for attempt := 0; attempt < 2; attempt++ {
		if e = ctx.Err(); e != nil {
			return e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, r.SourceURL, nil)
		if e != nil {
			return ErrSource
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			req.Header.Set("If-Range", v)
		}
		resp, e := m.client.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrSource
		}
		encoding := resp.Header.Get("Content-Encoding")
		if encoding != "" && !strings.EqualFold(encoding, "identity") {
			resp.Body.Close()
			return ErrSource
		}
		var total *int64
		restart := false
		switch resp.StatusCode {
		case http.StatusOK:
			if resp.Header.Get("Content-Range") != "" {
				resp.Body.Close()
				return ErrSource
			}
			offset = 0
			if resp.ContentLength >= 0 {
				n := resp.ContentLength
				total = &n
				if r.TotalBytes != nil && *r.TotalBytes != n && !recovery {
					resp.Body.Close()
					return ErrSource
				}
			} else {
				total = r.TotalBytes
				// A full response for a different (or unidentifiable) entity
				// cannot inherit the old entity's exact size.
				if recovery && (v == "" || (usableETag(r.ETag) && resp.Header.Get("ETag") != r.ETag) || (!usableETag(r.ETag) && resp.Header.Get("Last-Modified") != r.LastModified)) {
					total = nil
				}
				if restartTotal != nil && ((usableETag(restartETag) && resp.Header.Get("ETag") == restartETag) || (!usableETag(restartETag) && restartModified != "" && resp.Header.Get("Last-Modified") == restartModified)) {
					total = restartTotal
				}
			}
		case http.StatusPartialContent:
			start, end, size, ok := contentRange(resp.Header.Get("Content-Range"))
			if offset == 0 || !ok || start != offset || (resp.ContentLength >= 0 && resp.ContentLength != end-start+1) {
				resp.Body.Close()
				return ErrSource
			}
			changed := (usableETag(r.ETag) && resp.Header.Get("ETag") != r.ETag) || (!usableETag(r.ETag) && resp.Header.Get("Last-Modified") != r.LastModified) || (r.TotalBytes != nil && *r.TotalBytes != size)
			if changed {
				restartTotal = &size
				restartETag, restartModified = resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
				if _, err := http.ParseTime(restartModified); err != nil {
					restartModified = ""
				}
				restart = true
			} else {
				total = &size
			}
		case http.StatusRequestedRangeNotSatisfiable:
			if offset == 0 {
				resp.Body.Close()
				return ErrSource
			}
			restart = true
		default:
			resp.Body.Close()
			return ErrSource
		}
		if restart {
			resp.Body.Close()
			offset = 0
			v = ""
			continue
		}
		if e = m.checkSpace(total, offset); e != nil {
			resp.Body.Close()
			return e
		}
		e = m.writeBody(ctx, r, resp, offset, total)
		resp.Body.Close()
		return e
	}
	return ErrSource
}
func (m *Manager) writeBody(ctx context.Context, r *record, resp *http.Response, offset int64, total *int64) (result error) {
	if !m.owned(*r) {
		return ErrStorage
	}
	name := r.ID + ".part"
	before, e := m.root.Lstat(name)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return ErrStorage
	}
	if e == nil && !before.Mode().IsRegular() {
		return ErrStorage
	}
	file, e := m.root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return writeError(e)
	}
	defer func() {
		if e := file.Sync(); e != nil {
			result = errors.Join(result, writeError(e))
		}
		if info, e := file.Stat(); e == nil {
			r.DownloadedBytes = info.Size()
		} else {
			result = errors.Join(result, ErrStorage)
		}
		if e := file.Close(); e != nil {
			result = errors.Join(result, writeError(e))
		}
	}()
	after, e := file.Stat()
	if e != nil || !after.Mode().IsRegular() || (before != nil && !os.SameFile(before, after)) || after.Size() != r.DownloadedBytes {
		return ErrStorage
	}
	// The existing bytes remain untouched until both response and capacity pass.
	if offset == 0 {
		if e = file.Truncate(0); e != nil {
			return writeError(e)
		}
	}
	if _, e = file.Seek(offset, io.SeekStart); e != nil {
		return ErrStorage
	}
	r.DownloadedBytes = offset
	r.TotalBytes = total
	r.ETag = resp.Header.Get("ETag")
	r.LastModified = resp.Header.Get("Last-Modified")
	if e = m.save(r); e != nil {
		return ErrStorage
	}
	buf := make([]byte, 64*1024)
	checkpoint := time.Now()
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if total != nil && int64(n) > *total-r.DownloadedBytes {
				return ErrSource
			}
			written, writeErr := m.write(file, buf[:n])
			r.DownloadedBytes += int64(written)
			if writeErr != nil {
				return writeError(writeErr)
			}
			if written != n {
				return ErrStorage
			}
			if time.Since(checkpoint) >= time.Second {
				if e = m.save(r); e != nil {
					return ErrStorage
				}
				checkpoint = time.Now()
			}
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if readErr != io.EOF {
				return ErrSource
			}
			if total != nil && r.DownloadedBytes != *total {
				return ErrSource
			}
			if total == nil {
				n := r.DownloadedBytes
				r.TotalBytes = &n
			}
			return nil
		}
	}
}
func writeError(e error) error {
	if diskFull(e) {
		return ErrSpace
	}
	return ErrStorage
}
