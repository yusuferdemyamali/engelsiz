package providers

import (
	"context"
	"errors"
)

type SearchResult struct {
	Title     string `json:"title"`
	Snippet   string `json:"snippet"`
	Source    string `json:"source"`
	ArticleID string `json:"articleId"`
	Href      string `json:"href"`
}

type SearchResponse struct {
	Query        string         `json:"query"`
	PackageID    string         `json:"packageId"`
	TotalResults int            `json:"totalResults"`
	Results      []SearchResult `json:"results"`
}

// SearchProvider is optional so lifecycle-only providers need no search mock.
type SearchProvider interface {
	Search(context.Context, string, string) (SearchResponse, error)
}

var (
	ErrContentUnavailable = errors.New("selected content unavailable")
	ErrRuntimeUnavailable = errors.New("owned runtime unavailable")
	ErrSearchUnavailable  = errors.New("local search unavailable")
)
