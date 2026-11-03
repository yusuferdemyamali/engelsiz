// Package maps contains acquisition and validation logic for offline maps.
package maps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
)

// DefaultSource is a dated planet archive, not a URL supplied by an API caller.
// Deployments must be able to replace it when the provider retires the archive.
const DefaultSource = "https://build.protomaps.com/20261102.pmtiles"

const headerSize = 127

var contentRange = regexp.MustCompile(`^bytes 0-126/([0-9]+)$`)

// SourceInfo describes only facts verified from a remote PMTiles v3 header.
type SourceInfo struct {
	SizeBytes int64
	MinZoom   uint8
	MaxZoom   uint8
	TileType  uint8
}

// CheckSource checks that the server honors a bounded range request and that
// its response begins with a PMTiles v3 header. It does not download the planet
// or establish the integrity of the archive's tiles.
func CheckSource(ctx context.Context, client *http.Client, source string) (SourceInfo, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return SourceInfo{}, fmt.Errorf("create source request: %w", err)
	}
	req.Header.Set("Range", "bytes=0-126")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return SourceInfo{}, fmt.Errorf("request source header: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return SourceInfo{}, fmt.Errorf("source does not support byte ranges: HTTP %d", resp.StatusCode)
	}
	matches := contentRange.FindStringSubmatch(resp.Header.Get("Content-Range"))
	if matches == nil {
		return SourceInfo{}, fmt.Errorf("invalid source Content-Range")
	}
	size, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || size < headerSize {
		return SourceInfo{}, fmt.Errorf("invalid source size")
	}
	if resp.ContentLength != headerSize {
		return SourceInfo{}, fmt.Errorf("invalid source Content-Length")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, headerSize+1))
	if err != nil || len(data) != headerSize {
		return SourceInfo{}, fmt.Errorf("incomplete source header")
	}
	if string(data[:7]) != "PMTiles" || data[7] != 3 {
		return SourceInfo{}, fmt.Errorf("source is not PMTiles v3")
	}
	if data[100] > data[101] || data[101] > 31 {
		return SourceInfo{}, fmt.Errorf("invalid source zoom range")
	}
	return SourceInfo{SizeBytes: size, MinZoom: data[100], MaxZoom: data[101], TileType: data[99]}, nil
}
