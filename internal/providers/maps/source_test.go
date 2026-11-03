package maps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckSource(t *testing.T) {
	header := make([]byte, headerSize)
	copy(header, "PMTiles")
	header[7], header[99], header[100], header[101] = 3, 1, 0, 15
	for _, tc := range []struct {
		name        string
		status      int
		rangeHeader string
		body        []byte
		wantErr     bool
	}{
		{"valid", 206, "bytes 0-126/1000", header, false},
		{"ignored range", 200, "", header, true},
		{"wrong range", 206, "bytes 1-127/1000", header, true},
		{"short header", 206, "bytes 0-126/1000", header[:50], true},
		{"invalid magic", 206, "bytes 0-126/1000", make([]byte, headerSize), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=0-126" {
					t.Error("missing bounded range")
				}
				w.Header().Set("Content-Range", tc.rangeHeader)
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			}))
			defer s.Close()
			info, err := CheckSource(context.Background(), s.Client(), s.URL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if err == nil && (info.MaxZoom != 15 || info.SizeBytes != 1000 || info.TileType != 1) {
				t.Fatalf("unexpected header: %+v", info)
			}
		})
	}
}
