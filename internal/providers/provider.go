// Package providers defines the content boundary used by the application.
package providers

import (
	"context"
	"time"
)

type RuntimeState string

const (
	Stopped  RuntimeState = "stopped"
	Starting RuntimeState = "starting"
	Ready    RuntimeState = "ready"
	Stopping RuntimeState = "stopping"
	Failed   RuntimeState = "failed"
)

// RuntimeStatus is an internal value snapshot. It deliberately has no endpoint
// or content paths; public HTTP responses do not depend on runtime details.
type RuntimeStatus struct {
	State     RuntimeState
	PID       int
	LastError string
}

// CatalogPackage is upstream metadata, not a verified or installed artifact.
type CatalogPackage struct {
	Provider          string      `json:"provider"`
	ProviderPackageID string      `json:"providerPackageId"`
	Title             string      `json:"title"`
	Language          string      `json:"language"`
	Variant           string      `json:"variant"`
	Description       string      `json:"description,omitempty"`
	PublishedAt       *time.Time  `json:"publishedAt"`
	UpdatedAt         *time.Time  `json:"updatedAt"`
	SizeBytes         *int64      `json:"sizeBytes"`
	Download          Acquisition `json:"download"`
}

type Acquisition struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	MediaType string `json:"mediaType"`
}

// DownloadSource is trusted transfer metadata resolved by a provider. TotalBytes
// is nil when the provider cannot supply an exact payload length.
type DownloadSource struct {
	Provider          string
	ProviderPackageID string
	Title             string
	Variant           string
	FileName          string
	SourceURL         string
	TotalBytes        *int64
}

type AcquisitionResolver interface {
	ResolveDownload(context.Context, string) (DownloadSource, error)
}

type ContentProvider interface {
	Catalog(context.Context) ([]CatalogPackage, error)
	EnsureStarted(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
	Status() RuntimeStatus
}
