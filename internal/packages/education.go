package packages

import (
	"database/sql"
	"errors"
	"os"

	"engelsiz/internal/download"
)

// RemoveEducation removes exactly one installed educational ZIM record: the
// narrowly scoped variant guard refuses every non-education row (including
// other kiwix content), the activation row is removed before the file unlink
// (so a crash retains a recoverable record), then the file, then the durable
// record. The installed notification fires after the lock is released,
// following the repository.installed pattern, so observers can safely call
// back into the service. A second call for the same id reports not_found,
// making retry idempotent.
func (s *Service) RemoveEducation(id string) error {
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil || s.persistErr != nil {
		s.mu.Unlock()
		return download.ErrStorage
	}
	if !s.directoriesOwned() {
		s.mu.Unlock()
		return ErrUnsafe
	}
	r, err := s.get(id)
	if errors.Is(err, sql.ErrNoRows) {
		s.mu.Unlock()
		return download.ErrNotFound
	}
	if err != nil {
		s.mu.Unlock()
		return download.ErrStorage
	}
	// Narrow scope: only the namespaced educational ZIM variant is removable
	// here. Every other record (Wikipedia, updates, foreign providers, or an
	// educational row with an unexpected variant) is refused without touching
	// anything.
	if r.Provider != "kiwix" || r.ContentType != "education" || r.Variant != "edu-"+r.ProviderPackageID {
		s.mu.Unlock()
		return download.ErrInvalid
	}
	if !s.owned(&r) {
		s.mu.Unlock()
		return ErrUnsafe
	}
	if s.active[id] {
		s.mu.Unlock()
		return download.ErrState
	}
	delete(s.accepted, id)
	if _, err := s.db.Exec(`DELETE FROM active_packages WHERE package_id=?`, r.ID); err != nil {
		s.mu.Unlock()
		return download.ErrStorage
	}
	dst := r.ID + ".zim"
	if fi, err := s.target.Lstat(dst); err == nil {
		if !fi.Mode().IsRegular() {
			s.mu.Unlock()
			return ErrUnsafe
		}
		if err := s.target.Remove(dst); err != nil {
			s.mu.Unlock()
			return download.ErrStorage
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		return ErrUnsafe
	}
	res, err := s.db.Exec(`DELETE FROM content_packages WHERE id=? AND provider='kiwix' AND content_type='education' AND variant=?`, r.ID, r.Variant)
	if err != nil {
		s.mu.Unlock()
		return download.ErrStorage
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		s.mu.Unlock()
		return download.ErrStorage
	}
	notify := s.opts.OnInstalled
	s.mu.Unlock()
	if notify != nil {
		notify()
	}
	return nil
}
