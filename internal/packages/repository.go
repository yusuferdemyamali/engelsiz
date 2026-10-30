package packages

import (
	"database/sql"
	"engelsiz/internal/download"
	"errors"
	"os"
	"time"
)

const columns = `id,download_id,provider,provider_package_id,content_type,language,variant,title,version,source_url,source_path,local_path,size_bytes,checksum_algorithm,checksum_value,status,error_code,error_message,created_at,updated_at,installed_at`

type scanner interface{ Scan(...any) error }

func scan(s scanner) (record, error) {
	var r record
	var code, msg string
	var installed sql.NullString
	e := s.Scan(&r.ID, &r.DownloadID, &r.Provider, &r.ProviderPackageID, &r.ContentType, &r.Language, &r.Variant, &r.Title, &r.Version, &r.SourceURL, &r.SourcePath, &r.LocalPath, &r.SizeBytes, &r.Checksum.Algorithm, &r.Checksum.Value, &r.Status, &code, &msg, &r.CreatedAt, &r.UpdatedAt, &installed)
	if code != "" {
		r.Error = &download.Error{Code: code, Message: msg}
	}
	if installed.Valid {
		r.InstalledAt = &installed.String
	}
	return r, e
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (s *Service) get(id string) (record, error) {
	return scan(s.db.QueryRow(`SELECT `+columns+` FROM content_packages WHERE download_id=?`, id))
}
func (s *Service) records() ([]record, error) {
	rows, e := s.db.Query(`SELECT ` + columns + ` FROM content_packages ORDER BY created_at DESC,id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) insert(r *record) error {
	_, e := s.db.Exec(`INSERT INTO content_packages (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.DownloadID, r.Provider, r.ProviderPackageID, r.ContentType, r.Language, r.Variant, r.Title, r.Version, r.SourceURL, r.SourcePath, r.LocalPath, r.SizeBytes, r.Checksum.Algorithm, r.Checksum.Value, r.Status, "", "", r.CreatedAt, r.UpdatedAt, nil)
	return e
}
func (s *Service) save(r *record) error {
	r.UpdatedAt = now()
	code, msg := "", ""
	if r.Error != nil {
		code, msg = r.Error.Code, r.Error.Message
	}
	result, e := s.db.Exec(`UPDATE content_packages SET status=?,error_code=?,error_message=?,updated_at=?,installed_at=? WHERE id=?`, r.Status, code, msg, r.UpdatedAt, r.InstalledAt, r.ID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return sql.ErrNoRows
	}
	return e
}
func (s *Service) installed(r *record, verified os.FileInfo) error {
	if !s.directoriesOwned() {
		return s.fail(r, ErrUnsafe)
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	stamp := now()
	result, e := tx.Exec(`UPDATE downloads SET artifact_state='consumed' WHERE id=? AND artifact_owner=? AND status='completed' AND artifact_state IN ('claimed','consumed')`, r.DownloadID, r.ID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("artifact claim lost")
	}
	_, e = tx.Exec(`UPDATE content_packages SET status='installed',error_code='',error_message='',updated_at=?,installed_at=COALESCE(installed_at,?) WHERE id=?`, stamp, stamp, r.ID)
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.mu.Lock()
	s.accepted[r.ID] = verified
	s.mu.Unlock()
	if s.opts.OnInstalled != nil {
		s.opts.OnInstalled()
	}
	return nil
}
