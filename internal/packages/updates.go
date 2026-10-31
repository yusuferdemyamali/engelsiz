package packages

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"engelsiz/internal/download"
	"engelsiz/internal/providers"
)

// Update contains public metadata only. Selection is independent of file status.
type Update struct {
	ID                         string          `json:"id,omitempty"`
	PackageID                  string          `json:"packageId"`
	CandidateProviderPackageID string          `json:"candidateProviderPackageId"`
	DownloadID                 string          `json:"downloadId,omitempty"`
	CandidatePackageID         string          `json:"candidatePackageId,omitempty"`
	InstalledVersion           string          `json:"installedVersion"`
	CandidateVersion           string          `json:"candidateVersion"`
	SizeBytes                  *int64          `json:"sizeBytes"`
	State                      string          `json:"state"`
	Error                      *download.Error `json:"error,omitempty"`
}
type UpdateActivation struct{ ID, PackageID, CandidatePackageID string }

const updateColumns = `id,package_id,candidate_provider_package_id,COALESCE(download_id,''),installed_version,candidate_version,size_bytes,state,error_code,error_message`

func scanUpdate(row scanner) (Update, error) {
	var u Update
	var code, msg string
	err := row.Scan(&u.ID, &u.PackageID, &u.CandidateProviderPackageID, &u.DownloadID, &u.InstalledVersion, &u.CandidateVersion, &u.SizeBytes, &u.State, &code, &msg)
	if code != "" {
		u.Error = &download.Error{Code: code, Message: msg}
	}
	if u.DownloadID != "" {
		u.CandidatePackageID = u.DownloadID
	}
	return u, err
}
func (s *Service) update(id string) (Update, error) {
	return scanUpdate(s.db.QueryRow(`SELECT `+updateColumns+` FROM package_updates WHERE id=?`, id))
}
func (s *Service) Updates() ([]Update, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.persistErr != nil {
		return nil, download.ErrStorage
	}
	rows, e := s.db.Query(`SELECT ` + updateColumns + ` FROM package_updates ORDER BY created_at DESC,id DESC`)
	if e != nil {
		return nil, download.ErrStorage
	}
	defer rows.Close()
	out := []Update{}
	for rows.Next() {
		u, e := scanUpdate(rows)
		if e != nil {
			return nil, download.ErrStorage
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Service) recoverUpdates() error {
	// A reservation precedes Create. Recover its download identity by immutable
	// upstream release metadata; never select its file merely because it exists.
	type reservation struct{ id, candidate, version, created string }
	rows, e := s.db.Query(`SELECT id,candidate_provider_package_id,candidate_version,created_at FROM package_updates WHERE state='creating' AND download_id IS NULL`)
	if e != nil {
		return e
	}
	reservations := []reservation{}
	for rows.Next() {
		var r reservation
		if e = rows.Scan(&r.id, &r.candidate, &r.version, &r.created); e != nil {
			rows.Close()
			return e
		}
		reservations = append(reservations, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range reservations {
		created, err := time.Parse(time.RFC3339Nano, r.created)
		if err != nil {
			return err
		}
		downloads, err := s.db.Query(`SELECT id,created_at FROM downloads WHERE provider='kiwix' AND provider_package_id=? AND version=?`, r.candidate, r.version)
		if err != nil {
			return err
		}
		chosen := ""
		var earliest time.Time
		for downloads.Next() {
			var id, stamp string
			if err = downloads.Scan(&id, &stamp); err != nil {
				downloads.Close()
				return err
			}
			when, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil {
				downloads.Close()
				return err
			}
			if !when.Before(created) && (chosen == "" || when.Before(earliest)) {
				chosen = id
				earliest = when
			}
		}
		err = downloads.Err()
		downloads.Close()
		if err != nil {
			return err
		}
		if chosen != "" {
			if _, err = s.db.Exec(`UPDATE package_updates SET download_id=? WHERE id=? AND state='creating'`, chosen, r.id); err != nil {
				return err
			}
		}
	}
	_, e = s.db.Exec(`UPDATE package_updates SET state=CASE WHEN download_id IS NULL THEN 'cancelled' ELSE 'downloading' END,error_code=CASE WHEN download_id IS NULL THEN 'update_interrupted' ELSE '' END,error_message=CASE WHEN download_id IS NULL THEN 'Güncelleme başlatılırken kesildi. Yeniden Güncelle seçebilirsiniz.' ELSE '' END WHERE state='creating'`)
	if e != nil {
		return e
	}
	_, e = s.db.Exec(`UPDATE package_updates SET state='failed',error_code=(SELECT error_code FROM content_packages WHERE download_id=package_updates.download_id),error_message=(SELECT error_message FROM content_packages WHERE download_id=package_updates.download_id) WHERE state='downloading' AND download_id IN (SELECT download_id FROM content_packages WHERE status='failed')`)
	if e != nil {
		return e
	}
	_, e = s.db.Exec(`UPDATE package_updates SET state='interrupted',error_code='update_interrupted',error_message='Güncelleme etkinleştirilmeden kesildi. Önceki sürüm korunuyor; yeniden deneyebilirsiniz.' WHERE state IN ('ready','activating')`)
	return e
}

// NewerRelease compares publication dates, never lexical arbitrary versions.
func NewerRelease(base Package, candidate providers.DownloadSource) bool {
	old, e1 := time.Parse("2006-01-02", base.Version)
	next, e2 := time.Parse("2006-01-02", candidate.Version)
	return e1 == nil && e2 == nil && next.After(old) && base.Provider == candidate.Provider && base.ContentType == candidate.ContentType && base.Language == candidate.Language && base.Variant == candidate.Variant && base.ProviderPackageID != candidate.ProviderPackageID && candidate.ProviderPackageID != ""
}
func CompareUpdates(installed []Package, catalog []providers.CatalogPackage) []Update {
	out := []Update{}
	for _, base := range installed {
		if !base.Active || base.Status != "installed" {
			continue
		}
		var best *Update
		for _, candidate := range catalog {
			if candidate.PublishedAt == nil {
				continue
			}
			version := candidate.PublishedAt.Format("2006-01-02")
			source := providers.DownloadSource{Provider: candidate.Provider, ProviderPackageID: candidate.ProviderPackageID, ContentType: "wikipedia", Language: candidate.Language, Variant: candidate.Variant, Version: version}
			if !NewerRelease(base, source) {
				continue
			}
			if best == nil || version > best.CandidateVersion {
				best = &Update{PackageID: base.ID, InstalledVersion: base.Version, CandidateProviderPackageID: candidate.ProviderPackageID, CandidateVersion: version, SizeBytes: candidate.SizeBytes, State: "update_available"}
			}
		}
		if best != nil {
			out = append(out, *best)
		}
	}
	return out
}

func (s *Service) StartUpdate(ctx context.Context, m *download.Manager, resolver providers.AcquisitionResolver, baseID, candidateID string) (Update, error) {
	if resolver == nil {
		return Update{}, download.ErrSource
	}
	// Resolve outside registry locks; active selection is checked again below.
	source, e := resolver.ResolveDownload(ctx, candidateID)
	if e != nil {
		return Update{}, download.ErrSource
	}
	if source.ProviderPackageID != candidateID {
		return Update{}, download.ErrInvalid
	}
	artifact := download.Artifact{Descriptor: download.Descriptor{Provider: source.Provider, ProviderPackageID: source.ProviderPackageID, ContentType: source.ContentType, Language: source.Language, Variant: source.Variant, Title: source.Title, Version: source.Version, ExpectedSize: source.ExpectedSize, ChecksumAlgorithm: source.ChecksumAlgorithm, ChecksumValue: source.ChecksumValue}}
	if e = validMetadata(artifact); e != nil {
		return Update{}, e
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil || s.persistErr != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	base, e := s.get(baseID)
	if e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrNotFound
	}
	var active int
	e = s.db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, baseID).Scan(&active)
	if e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	if active != 1 || base.Status != "installed" || !NewerRelease(base.Package, source) {
		s.mu.Unlock()
		return Update{}, download.ErrState
	}
	selected, snapshotErr := s.snapshot(true)
	if snapshotErr != nil {
		s.mu.Unlock()
		return Update{}, snapshotErr
	}
	usable := false
	for _, file := range selected {
		if file.PackageID == baseID {
			usable = true
		}
	}
	if !usable {
		s.mu.Unlock()
		return Update{}, download.ErrState
	}
	// Reconcile cancelled transfers before evaluating uniqueness; failed transfers
	// retain the operation so their existing retry controls remain authoritative.
	_, e = s.db.Exec(`UPDATE package_updates SET state='cancelled' WHERE package_id=? AND ((state='downloading' AND download_id IN (SELECT id FROM downloads WHERE status='cancelled')) OR (state='failed' AND download_id IN (SELECT download_id FROM content_packages WHERE status='failed')))`, baseID)
	if e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	// A known unusable or interrupted candidate must not prevent a later
	// upstream release from being tried. Preserve its operation/file history.
	_, e = s.db.Exec(`UPDATE package_updates SET state='cancelled',updated_at=? WHERE package_id=? AND state IN ('failed','interrupted') AND candidate_provider_package_id!=? AND candidate_version<?`, now(), baseID, candidateID, source.Version)
	if e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	var live int
	e = s.db.QueryRow(`SELECT COUNT(*) FROM package_updates WHERE package_id=? AND state NOT IN ('succeeded','cancelled')`, baseID).Scan(&live)
	if e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	if live > 0 {
		s.mu.Unlock()
		return Update{}, download.ErrExists
	}
	id := make([]byte, 16)
	if _, e = rand.Read(id); e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	u := Update{ID: hex.EncodeToString(id), PackageID: baseID, CandidateProviderPackageID: candidateID, InstalledVersion: base.Version, CandidateVersion: source.Version, SizeBytes: source.ExpectedSize, State: "creating"}
	stamp := now()
	_, e = s.db.Exec(`INSERT INTO package_updates(id,package_id,candidate_provider_package_id,installed_version,candidate_version,size_bytes,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, u.ID, baseID, candidateID, base.Version, source.Version, source.ExpectedSize, "creating", stamp, stamp)
	s.mu.Unlock()
	if e != nil {
		return Update{}, download.ErrStorage
	}
	d, e := m.Create(download.Descriptor{Provider: source.Provider, ProviderPackageID: candidateID, Title: source.Title, Variant: source.Variant, FileName: source.FileName, SourceURL: source.SourceURL, TotalBytes: source.TotalBytes, ContentType: source.ContentType, Language: source.Language, Version: source.Version, ExpectedSize: source.ExpectedSize, ChecksumAlgorithm: source.ChecksumAlgorithm, ChecksumValue: source.ChecksumValue})
	if e != nil {
		_, saveErr := s.db.Exec(`UPDATE package_updates SET state='cancelled',error_code='update_start_failed',error_message='Güncelleme indirmesi başlatılamadı. Yeniden deneyebilirsiniz.',updated_at=? WHERE id=?`, now(), u.ID)
		if saveErr != nil {
			return Update{}, download.ErrStorage
		}
		return Update{}, e
	}
	// The reservation excludes this identity before Create can start its worker.
	// Attach only if install hasn't already advanced the operation to ready.
	_, e = s.db.Exec(`UPDATE package_updates SET download_id=?,state=CASE WHEN state='creating' THEN 'downloading' ELSE state END,updated_at=? WHERE id=?`, d.ID, now(), u.ID)
	if e != nil {
		return Update{}, download.ErrStorage
	}
	return s.update(u.ID)
}
func (s *Service) PendingActivations() ([]UpdateActivation, error) {
	ops, e := s.Updates()
	if e != nil {
		return nil, e
	}
	out := []UpdateActivation{}
	for _, u := range ops {
		if u.State == "ready" {
			out = append(out, UpdateActivation{u.ID, u.PackageID, u.DownloadID})
		}
	}
	return out, nil
}
func (s *Service) BeginActivation(id string) ([]InstalledFile, InstalledFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil || s.persistErr != nil {
		return nil, InstalledFile{}, download.ErrStorage
	}
	u, e := s.update(id)
	if e != nil {
		return nil, InstalledFile{}, download.ErrNotFound
	}
	if u.State != "ready" {
		return nil, InstalledFile{}, download.ErrState
	}
	previous, e := s.snapshot(true)
	if e != nil {
		return nil, InstalledFile{}, e
	}
	found := false
	for _, f := range previous {
		if f.PackageID == u.PackageID {
			found = true
		}
	}
	if !found {
		return nil, InstalledFile{}, download.ErrState
	}
	files, e := s.snapshot(false)
	if e != nil {
		return nil, InstalledFile{}, e
	}
	var candidate InstalledFile
	for _, f := range files {
		if f.PackageID == u.DownloadID {
			candidate = f
		}
	}
	if candidate.PackageID == "" {
		return nil, InstalledFile{}, download.ErrState
	}
	_, e = s.db.Exec(`UPDATE package_updates SET state='activating',error_code='',error_message='',updated_at=? WHERE id=? AND state='ready'`, now(), id)
	return previous, candidate, e
}
func (s *Service) CommitActivation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return download.ErrStorage
	}
	u, e := s.update(id)
	if e != nil {
		return download.ErrNotFound
	}
	if u.State != "activating" {
		return download.ErrState
	}
	// Recheck candidate ownership immediately before publishing the selection.
	files, e := s.snapshot(false)
	if e != nil {
		return e
	}
	valid := false
	for _, f := range files {
		if f.PackageID == u.DownloadID {
			valid = true
		}
	}
	if !valid {
		return ErrUnsafe
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.Exec(`DELETE FROM active_packages WHERE package_id=?`, u.PackageID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return download.ErrState
	}
	if _, e = tx.Exec(`INSERT INTO active_packages(package_id) VALUES(?)`, u.DownloadID); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE package_updates SET state='succeeded',error_code='',error_message='',updated_at=? WHERE id=? AND state='activating'`, now(), id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) FailActivation(id, code, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, e := s.db.Exec(`UPDATE package_updates SET state='failed',error_code=?,error_message=?,updated_at=? WHERE id=? AND state='activating'`, code, message, now(), id)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return download.ErrState
	}
	return e
}
func (s *Service) RetryActivation(id string) (Update, error) {
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		return Update{}, download.ErrStorage
	}
	u, e := s.update(id)
	if e != nil {
		s.mu.Unlock()
		return Update{}, download.ErrNotFound
	}
	if u.State != "failed" && u.State != "interrupted" && u.State != "ready" {
		s.mu.Unlock()
		return Update{}, download.ErrState
	}
	candidate, e := s.get(u.DownloadID)
	if e != nil || candidate.Status != "installed" {
		s.mu.Unlock()
		return Update{}, download.ErrState
	}
	_, e = s.db.Exec(`UPDATE package_updates SET state='ready',error_code='',error_message='',updated_at=? WHERE id=?`, now(), id)
	s.mu.Unlock()
	if e != nil {
		return Update{}, download.ErrStorage
	}
	if s.opts.OnInstalled != nil {
		s.opts.OnInstalled()
	}
	return s.update(id)
}
