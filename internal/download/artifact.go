package download

// Artifact is an internal immutable acquisition snapshot. It is never an API
// response; the consumer owns verification and finalization, not the manager.
type Artifact struct {
	ID string
	Descriptor
	TempPath string
}

var ErrOwnership = &Error{"ownership_conflict", "Dosya başka bir kurulum tarafından kullanılıyor veya güvenli değil."}

// ClaimCompleted reserves a completed artifact in a short operation. In
// particular, no hashing or filesystem finalization happens while ops is held.
func (m *Manager) ClaimCompleted(id, owner string) (Artifact, error) {
	m.ops.Lock()
	defer m.ops.Unlock()
	if e := m.healthy(); e != nil {
		return Artifact{}, e
	}
	r, e := m.get(id)
	if e != nil {
		return Artifact{}, publicError(e)
	}
	if r.Status != "completed" {
		return Artifact{}, ErrState
	}
	if !m.owned(r) || owner == "" || (r.ArtifactOwner != "" && r.ArtifactOwner != owner) || r.ArtifactState == "consumed" {
		return Artifact{}, ErrOwnership
	}
	info, e := m.root.Lstat(r.ID + ".part")
	if e != nil || !info.Mode().IsRegular() {
		return Artifact{}, ErrOwnership
	}
	if _, e = m.stat(r); e != nil {
		return Artifact{}, ErrOwnership
	}
	result, e := m.db.Exec(`UPDATE downloads SET artifact_owner=?,artifact_state='claimed' WHERE id=? AND status='completed' AND (artifact_owner IS NULL OR artifact_owner='' OR artifact_owner=?) AND artifact_state!='consumed'`, owner, id, owner)
	if e != nil {
		return Artifact{}, ErrStorage
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return Artifact{}, ErrOwnership
	}
	return Artifact{ID: r.ID, TempPath: r.TempPath, Descriptor: Descriptor{Provider: r.Provider, ProviderPackageID: r.ProviderPackageID, Title: r.Title, Variant: r.Variant, FileName: r.FileName, SourceURL: r.SourceURL, TotalBytes: r.TotalBytes, ContentType: r.ContentType, Language: r.Language, Version: r.Version, ExpectedSize: r.ExpectedSize, ChecksumAlgorithm: r.ChecksumAlgorithm, ChecksumValue: r.ChecksumValue}}, nil
}
