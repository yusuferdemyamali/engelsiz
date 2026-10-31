package packages

import (
	"engelsiz/internal/download"
	"os"
	"sort"
)

// InstalledFile is a private activation input, never a public API object.
type InstalledFile struct{ PackageID, Path string }

// InstalledSnapshot uses only reconciled/committed identities. It never hashes
// content or discovers files outside the durable registry.
func (s *Service) InstalledSnapshot() ([]InstalledFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.persistErr != nil {
		return nil, download.ErrStorage
	}
	if !s.directoriesOwned() {
		return nil, download.ErrStorage
	}
	return s.snapshot(true)
}

func (s *Service) snapshot(activeOnly bool) ([]InstalledFile, error) {
	rows, err := s.records()
	if err != nil {
		return nil, download.ErrStorage
	}
	out := []InstalledFile{}
	for _, r := range rows {
		if activeOnly {
			var selected int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, r.ID).Scan(&selected); err != nil {
				return nil, download.ErrStorage
			}
			if selected == 0 {
				continue
			}
		}
		accepted := s.accepted[r.ID]
		if r.Status != "installed" || accepted == nil || !s.owned(&r) {
			continue
		}
		current, err := s.target.Lstat(r.ID + ".zim")
		if err != nil || !current.Mode().IsRegular() || !os.SameFile(accepted, current) || current.Size() != accepted.Size() || !current.ModTime().Equal(accepted.ModTime()) {
			continue
		}
		out = append(out, InstalledFile{PackageID: r.ID, Path: r.LocalPath})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PackageID < out[j].PackageID })
	return out, nil
}
