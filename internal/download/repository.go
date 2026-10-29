package download

import (
	"database/sql"
	"errors"
	"time"
)

const columns = `id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,etag,last_modified,error_code,error_message,created_at,updated_at,COALESCE(content_type,''),COALESCE(language,''),COALESCE(version,''),expected_size,COALESCE(checksum_algorithm,''),COALESCE(checksum_value,''),COALESCE(artifact_owner,''),artifact_state`

type scanner interface{ Scan(...any) error }

func scan(s scanner) (record, error) {
	var r record
	var total, expected sql.NullInt64
	var code, message string
	e := s.Scan(&r.ID, &r.Provider, &r.ProviderPackageID, &r.Title, &r.Variant, &r.FileName, &r.SourceURL, &r.TempPath, &total, &r.DownloadedBytes, &r.Status, &r.ETag, &r.LastModified, &code, &message, &r.CreatedAt, &r.UpdatedAt, &r.ContentType, &r.Language, &r.Version, &expected, &r.ChecksumAlgorithm, &r.ChecksumValue, &r.ArtifactOwner, &r.ArtifactState)
	if expected.Valid {
		r.ExpectedSize = &expected.Int64
	}
	if total.Valid {
		r.TotalBytes = &total.Int64
	}
	if code != "" {
		r.Error = &Error{code, message}
	}
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, e
}
func (m *Manager) get(id string) (record, error) {
	return scan(m.db.QueryRow(`SELECT `+columns+` FROM downloads WHERE id=?`, id))
}
func (m *Manager) list() ([]record, error) {
	rows, e := m.db.Query(`SELECT ` + columns + ` FROM downloads ORDER BY created_at DESC,id DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	records := []record{}
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return nil, e
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
func (m *Manager) insert(r *record) error {
	_, e := m.db.Exec(`INSERT INTO downloads (id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,etag,last_modified,error_code,error_message,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.Provider, r.ProviderPackageID, r.Title, r.Variant, r.FileName, r.SourceURL, r.TempPath, r.TotalBytes, r.DownloadedBytes, r.Status, r.ETag, r.LastModified, "", "", r.CreatedAt, r.UpdatedAt, r.ContentType, r.Language, r.Version, r.ExpectedSize, r.ChecksumAlgorithm, r.ChecksumValue)
	return e
}
func (m *Manager) save(r *record) error {
	r.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	code, message := "", ""
	if r.Error != nil {
		code, message = r.Error.Code, r.Error.Message
	}
	result, e := m.db.Exec(`UPDATE downloads SET total_bytes=?,downloaded_bytes=?,status=?,etag=?,last_modified=?,error_code=?,error_message=?,updated_at=? WHERE id=?`, r.TotalBytes, r.DownloadedBytes, r.Status, r.ETag, r.LastModified, code, message, r.UpdatedAt, r.ID)
	if e != nil {
		return e
	}
	count, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}
