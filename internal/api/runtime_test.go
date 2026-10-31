package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type runtimeStub struct {
	err   error
	calls int
}

func (s *runtimeStub) Snapshot() RuntimeResponse {
	return RuntimeResponse{Runtime: RuntimeView{State: "stopped"}, Contents: []ContentLink{}}
}
func (s *runtimeStub) Retry() error { s.calls++; return s.err }
func TestRuntimeContract(t *testing.T) {
	for _, tc := range []struct {
		retry                bool
		method, target, body string
		err                  error
		status               int
	}{
		{false, "GET", "/api/runtime", "", nil, 200}, {false, "GET", "/api/runtime?x=1", "", nil, 400}, {false, "POST", "/api/runtime", "{}", nil, 405},
		{true, "POST", "/api/runtime/retry", "{}", nil, 202}, {true, "POST", "/api/runtime/retry", "{}", ErrNoInstalledContent, 409}, {true, "POST", "/api/runtime/retry", "{}", errors.New("private db path"), 500},
		{true, "POST", "/api/runtime/retry?x=1", "{}", nil, 400}, {true, "POST", "/api/runtime/retry", "null", nil, 400}, {true, "POST", "/api/runtime/retry", "{\"target\":\"remote\"}", nil, 400}, {true, "POST", "/api/runtime/retry", "{} {}", nil, 400}, {true, "GET", "/api/runtime/retry", "", nil, 405},
	} {
		t.Run(tc.method+tc.target+tc.body, func(t *testing.T) {
			stub := &runtimeStub{err: tc.err}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			RuntimeHandler(stub, tc.retry)(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "private db path") {
				t.Fatal("private diagnostic")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable")
			}
		})
	}
}
