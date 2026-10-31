package packages

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"engelsiz/internal/download"
	"engelsiz/internal/providers"
)

type updateResolver struct {
	source providers.DownloadSource
	err    error
}

func (r updateResolver) ResolveDownload(context.Context, string) (providers.DownloadSource, error) {
	return r.source, r.err
}

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func waitPackage(t *testing.T, s *Service, id, status string) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		p, e := s.get(id)
		if e == nil && p.Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("package %s never became %s", id, status)
}
func installedBase(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, Options{})
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, testID, "installed")
	return f
}
func sourceFor(body string) providers.DownloadSource {
	sum := sha256.Sum256([]byte(body))
	size := int64(len(body))
	return providers.DownloadSource{Provider: "kiwix", ProviderPackageID: "release-new", ContentType: "wikipedia", Language: "tr", Variant: "nopic", Title: "Wikipedia", Version: "2026-11-01", FileName: "new.zim", SourceURL: "https://example.invalid/new.zim", ExpectedSize: &size, TotalBytes: &size, ChecksumAlgorithm: "sha256", ChecksumValue: hex.EncodeToString(sum[:])}
}
func startCandidate(t *testing.T, f *fixture, body string) (Update, *download.Manager) {
	t.Helper()
	f.m.Close()
	m, e := download.New(f.db, filepath.Join(f.dir, "downloads"), download.Options{HTTPClient: &http.Client{Transport: updateTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
	})}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	u, e := f.s.StartUpdate(context.Background(), m, updateResolver{source: sourceFor(body)}, testID, "release-new")
	if e != nil {
		t.Fatal(e)
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		d, e := m.Get(u.DownloadID)
		if e == nil && d.Status == "completed" {
			return u, m
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("transfer not completed")
	return Update{}, nil
}
func TestUpdateSelectionCommitAndOfflineRestart(t *testing.T) {
	f := installedBase(t)
	u, m := startCandidate(t, f, "new verified ZIM")
	if _, e := f.s.StartUpdate(context.Background(), m, updateResolver{source: sourceFor("new verified ZIM")}, testID, "release-new"); !errors.Is(e, download.ErrExists) {
		t.Fatalf("duplicate %v", e)
	}
	if _, _, e := f.s.Install(m, u.DownloadID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, u.DownloadID, "installed")
	active, e := f.s.InstalledSnapshot()
	if e != nil || len(active) != 1 || active[0].PackageID != testID {
		t.Fatalf("candidate published early %+v %v", active, e)
	}
	pending, e := f.s.PendingActivations()
	if e != nil || len(pending) != 1 {
		t.Fatalf("pending %+v %v", pending, e)
	}
	previous, candidate, e := f.s.BeginActivation(u.ID)
	if e != nil || len(previous) != 1 || candidate.PackageID != u.DownloadID {
		t.Fatalf("prepare %+v %+v %v", previous, candidate, e)
	}
	if e = f.s.CommitActivation(u.ID); e != nil {
		t.Fatal(e)
	}
	active, e = f.s.InstalledSnapshot()
	if e != nil || len(active) != 1 || active[0].PackageID != u.DownloadID {
		t.Fatalf("commit %+v %v", active, e)
	}
	f.s.Close()
	reopened, e := New(context.Background(), f.db, f.dir, Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	active, e = reopened.InstalledSnapshot()
	if e != nil || len(active) != 1 || active[0].PackageID != u.DownloadID {
		t.Fatalf("restart %+v %v", active, e)
	}
	history, e := reopened.List("installed")
	if e != nil || len(history) != 2 {
		t.Fatalf("history %+v %v", history, e)
	}
	for _, p := range history {
		if p.Active != (p.ID == u.DownloadID) {
			t.Fatalf("active flag %+v", p)
		}
	}
}
func TestInterruptedAndFailedActivationNeverAutoPublish(t *testing.T) {
	for _, boundary := range []string{"ready", "activating", "failed"} {
		t.Run(boundary, func(t *testing.T) {
			f := installedBase(t)
			u, m := startCandidate(t, f, "new verified ZIM")
			if _, _, e := f.s.Install(m, u.DownloadID); e != nil {
				t.Fatal(e)
			}
			waitPackage(t, f.s, u.DownloadID, "installed")
			if boundary != "ready" {
				if _, _, e := f.s.BeginActivation(u.ID); e != nil {
					t.Fatal(e)
				}
			}
			if boundary == "failed" {
				if e := f.s.FailActivation(u.ID, "candidate_unusable", "Yeni sürüm açılamadı."); e != nil {
					t.Fatal(e)
				}
			}
			f.s.Close()
			s, e := New(context.Background(), f.db, f.dir, Options{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			active, e := s.InstalledSnapshot()
			if e != nil || len(active) != 1 || active[0].PackageID != testID {
				t.Fatalf("published %+v %v", active, e)
			}
			pending, e := s.PendingActivations()
			if e != nil || len(pending) != 0 {
				t.Fatalf("auto retry %+v %v", pending, e)
			}
			ops, e := s.Updates()
			if e != nil || len(ops) != 1 {
				t.Fatal(e)
			}
			expected := "interrupted"
			if boundary == "failed" {
				expected = "failed"
			}
			if ops[0].State != expected {
				t.Fatalf("state %+v", ops[0])
			}
			if _, e = s.RetryActivation(u.ID); e != nil {
				t.Fatal(e)
			}
			if _, _, e = s.BeginActivation(u.ID); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestUpdateReservationSurvivesCreateAttachGap(t *testing.T) {
	f := installedBase(t)
	candidateID := "fedcba9876543210fedcba9876543210"
	stamp := "2026-10-30T10:00:00.1Z"
	_, e := f.db.Exec(`INSERT INTO package_updates(id,package_id,candidate_provider_package_id,installed_version,candidate_version,state,created_at,updated_at) VALUES('gap',?,'new','2026-10-01','2026-11-01','creating',?,?)`, testID, stamp, stamp)
	if e != nil {
		t.Fatal(e)
	}
	f.seed(t, candidateID, "new", f.body)
	if _, e = f.db.Exec(`UPDATE downloads SET version='2026-11-01',created_at='2026-10-30T10:00:00.11Z' WHERE id=?`, candidateID); e != nil {
		t.Fatal(e)
	}
	f.s.Close()
	s, e := New(context.Background(), f.db, f.dir, Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ops, e := s.Updates()
	if e != nil || len(ops) != 1 || ops[0].DownloadID != candidateID {
		t.Fatalf("gap recovered %+v %v", ops, e)
	}
	if _, _, e = s.Install(f.m, candidateID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, s, candidateID, "installed")
	active, e := s.InstalledSnapshot()
	if e != nil || len(active) != 1 || active[0].PackageID != testID {
		t.Fatalf("gap published %+v %v", active, e)
	}
}
func TestUpdateValidationAndConcurrentReservation(t *testing.T) {
	f := installedBase(t)
	source := sourceFor("new")
	bad := source
	bad.ChecksumValue = ""
	if _, e := f.s.StartUpdate(context.Background(), f.m, updateResolver{source: bad}, testID, "release-new"); !errors.Is(e, ErrChecksum) {
		t.Fatalf("checksum %v", e)
	}
	bad = source
	bad.Variant = "mini"
	if _, e := f.s.StartUpdate(context.Background(), f.m, updateResolver{source: bad}, testID, "release-new"); !errors.Is(e, download.ErrState) {
		t.Fatalf("variant %v", e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.s.StartUpdate(context.Background(), f.m, updateResolver{source: source}, testID, "release-new")
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, download.ErrExists) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
func TestCompareUpdatesStrictCompatibleDates(t *testing.T) {
	base := Package{Active: true, ID: "old", Status: "installed", Provider: "kiwix", ProviderPackageID: "old-upstream", ContentType: "wikipedia", Language: "tr", Variant: "nopic", Version: "2026-10-01"}
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	candidate := providers.CatalogPackage{Provider: "kiwix", ProviderPackageID: "new", Language: "tr", Variant: "nopic", PublishedAt: &date}
	if offers := CompareUpdates([]Package{base}, []providers.CatalogPackage{candidate}); len(offers) != 1 || offers[0].State != "update_available" {
		t.Fatalf("offers %+v", offers)
	}
	for _, mutate := range []func(*Package, *providers.CatalogPackage){func(b *Package, c *providers.CatalogPackage) { b.Version = "unknown" }, func(b *Package, c *providers.CatalogPackage) { b.Version = "2026-11-01" }, func(b *Package, c *providers.CatalogPackage) { c.Language = "en" }, func(b *Package, c *providers.CatalogPackage) { c.Variant = "mini" }, func(b *Package, c *providers.CatalogPackage) { c.ProviderPackageID = b.ProviderPackageID }, func(b *Package, c *providers.CatalogPackage) { c.PublishedAt = nil }, func(b *Package, c *providers.CatalogPackage) { b.Active = false }} {
		b, c := base, candidate
		mutate(&b, &c)
		if offers := CompareUpdates([]Package{b}, []providers.CatalogPackage{c}); len(offers) != 0 {
			t.Fatalf("unsafe offer %+v", offers)
		}
	}
}

func TestUpdateVerificationFailurePreservesOldAndAllowsNewAttempt(t *testing.T) {
	f := installedBase(t)
	u, m := startCandidate(t, f, "bad candidate bytes")
	// Immutable trusted checksum differs from bytes; completed transfer must not
	// publish the candidate or change the previous selection.
	if _, e := f.db.Exec(`UPDATE downloads SET checksum_value=? WHERE id=?`, strings.Repeat("0", 64), u.DownloadID); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.Install(m, u.DownloadID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, u.DownloadID, "failed")
	ops, e := f.s.Updates()
	if e != nil || len(ops) != 1 || ops[0].State != "failed" || ops[0].Error == nil {
		t.Fatalf("verification state %+v %v", ops, e)
	}
	active, e := f.s.InstalledSnapshot()
	if e != nil || len(active) != 1 || active[0].PackageID != testID {
		t.Fatalf("old selection %+v %v", active, e)
	}
	source := sourceFor("replacement")
	source.ProviderPackageID = "replacement-upstream"
	source.Version = "2026-12-01"
	next, e := f.s.StartUpdate(context.Background(), m, updateResolver{source: source}, testID, source.ProviderPackageID)
	if e != nil || next.ID == u.ID {
		t.Fatalf("new attempt %+v %v", next, e)
	}
}
func TestCommitPreservesUnrelatedVariantAndRechecksOwnership(t *testing.T) {
	f := installedBase(t)
	otherID := "11111111111111111111111111111111"
	f.seed(t, otherID, "other-mini", f.body)
	if _, e := f.db.Exec(`UPDATE downloads SET variant='mini' WHERE id=?`, otherID); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.Install(f.m, otherID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, otherID, "installed")
	u, m := startCandidate(t, f, "new verified ZIM")
	if _, _, e := f.s.Install(m, u.DownloadID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, u.DownloadID, "installed")
	previous, _, e := f.s.BeginActivation(u.ID)
	if e != nil || len(previous) != 2 {
		t.Fatalf("previous %+v %v", previous, e)
	}
	if e = f.s.CommitActivation(u.ID); e != nil {
		t.Fatal(e)
	}
	selected, e := f.s.InstalledSnapshot()
	if e != nil || len(selected) != 2 {
		t.Fatalf("selection %+v %v", selected, e)
	}
	ids := map[string]bool{}
	for _, file := range selected {
		ids[file.PackageID] = true
	}
	if !ids[otherID] || !ids[u.DownloadID] || ids[testID] {
		t.Fatalf("wrong selection %+v", ids)
	}
}

func TestFailedCandidateCanBeSupersededByLaterUpstreamRelease(t *testing.T) {
	f := installedBase(t)
	u, m := startCandidate(t, f, "verified unusable ZIM")
	if _, _, e := f.s.Install(m, u.DownloadID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, u.DownloadID, "installed")
	if _, _, e := f.s.BeginActivation(u.ID); e != nil {
		t.Fatal(e)
	}
	if e := f.s.FailActivation(u.ID, "candidate_unusable", "Yeni sürüm açılamadı."); e != nil {
		t.Fatal(e)
	}
	source := sourceFor("new release")
	source.ProviderPackageID = "fixed-upstream"
	source.Version = "2026-12-01"
	next, e := f.s.StartUpdate(context.Background(), m, updateResolver{source: source}, testID, source.ProviderPackageID)
	if e != nil || next.ID == u.ID {
		t.Fatalf("supersede %+v %v", next, e)
	}
	ops, e := f.s.Updates()
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, op := range ops {
		if op.ID == u.ID {
			found = true
			if op.State != "cancelled" || op.Error == nil {
				t.Fatalf("history lost %+v", op)
			}
		}
	}
	if !found {
		t.Fatal("old operation removed")
	}
	selected, e := f.s.InstalledSnapshot()
	if e != nil || len(selected) != 1 || selected[0].PackageID != testID {
		t.Fatalf("selected %+v %v", selected, e)
	}
	retained, e := f.s.List("installed")
	if e != nil || len(retained) != 2 {
		t.Fatalf("old file lost %+v %v", retained, e)
	}
}

func TestPersistedInstallingCandidateRestartDoesNotAutoActivate(t *testing.T) {
	f := installedBase(t)
	u, m := startCandidate(t, f, "candidate before commit")
	// Simulate abrupt process loss immediately before installed transaction, after
	// atomic rename. Generic install recovery may finish, but startup must keep it
	// staged and require an explicit activation retry.
	entered := make(chan struct{})
	f.s.opts.Hook = func(ctx context.Context, stage string) error {
		if stage == "commit" {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	if _, _, e := f.s.Install(m, u.DownloadID); e != nil {
		t.Fatal(e)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("candidate did not reach commit boundary")
	}
	f.s.Close()
	notifications := 0
	s, e := New(context.Background(), f.db, f.dir, Options{OnInstalled: func() { notifications++ }})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if notifications == 0 {
		t.Fatal("test did not exercise reconciliation callback")
	}
	selected, e := s.InstalledSnapshot()
	if e != nil || len(selected) != 1 || selected[0].PackageID != testID {
		t.Fatalf("restart selected candidate %+v %v", selected, e)
	}
	pending, e := s.PendingActivations()
	if e != nil || len(pending) != 0 {
		t.Fatalf("startup auto activation %+v %v", pending, e)
	}
	ops, e := s.Updates()
	if e != nil || len(ops) != 1 || ops[0].State != "interrupted" {
		t.Fatalf("recovery state %+v %v", ops, e)
	}
}

func TestVerificationDiagnosticRecordsCodeWithoutArtifactContent(t *testing.T) {
	var logs bytes.Buffer
	f := newFixture(t, Options{Logger: log.New(&logs, "", 0)})
	if _, e := f.db.Exec(`UPDATE downloads SET checksum_value=? WHERE id=?`, strings.Repeat("0", 64), testID); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f.s, testID, "failed")
	text := logs.String()
	if !strings.Contains(text, testID) || !strings.Contains(text, "verification_failed") {
		t.Fatalf("missing verification diagnosis %s", text)
	}
	for _, private := range []string{string(f.body), f.dir, "upstream.invalid", "source_url"} {
		if strings.Contains(text, private) {
			t.Fatalf("verification log leaked %q", private)
		}
	}
}
