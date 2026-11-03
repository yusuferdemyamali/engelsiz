package maps

import (
	"errors"
	"strings"

	"engelsiz/internal/download"
)

const downloadPrefix = "maps:"

func (s *Service) ListDownloads() ([]download.Snapshot, error) {
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []download.Snapshot
	for _, item := range items {
		if item.Status != "available" && item.Status != "ready" {
			out = append(out, snapshot(item))
		}
	}
	return out, nil
}
func snapshot(item Region) download.Snapshot {
	status := "downloading"
	switch item.Status {
	case "queued":
		status = "queued"
	case "error":
		status = "failed"
	}
	var problem *download.Error
	if item.Error != "" {
		problem = &download.Error{Code: "map_extraction_failed", Message: item.Error}
	}
	return download.Snapshot{ID: downloadPrefix + item.ID, Provider: "maps", ProviderPackageID: item.ID, Title: item.Title, Variant: "Z0–Z15", FileName: item.ID + ".pmtiles", Status: status, Indeterminate: true, Error: problem}
}
func (s *Service) GetDownload(id string) (download.Snapshot, error) {
	if !strings.HasPrefix(id, downloadPrefix) {
		return download.Snapshot{}, download.ErrNotFound
	}
	r, err := find(strings.TrimPrefix(id, downloadPrefix))
	if err != nil {
		return download.Snapshot{}, download.ErrNotFound
	}
	r, err = s.listOne(r)
	if err != nil {
		return download.Snapshot{}, download.ErrStorage
	}
	if r.Status == "available" || r.Status == "ready" {
		return download.Snapshot{}, download.ErrNotFound
	}
	return snapshot(r), nil
}
func (s *Service) ControlDownload(id, action string) (download.Snapshot, error) {
	item, err := s.GetDownload(id)
	if err != nil {
		return item, err
	}
	switch action {
	case "cancel":
		err = s.Cancel(item.ProviderPackageID)
	case "retry":
		err = s.Start(item.ProviderPackageID)
	default:
		return item, download.ErrState
	}
	if errors.Is(err, ErrState) {
		return item, download.ErrState
	}
	if err != nil {
		return item, download.ErrStorage
	}
	return s.GetDownload(id)
}
