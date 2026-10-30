## Context

See proposal.md for motivation. Change 05's package service reconciles and rehashes installed records during `packages.New`; private records contain canonical `LocalPath`, while public packages intentionally omit it. Runtime `Options.ZIMPaths` is currently immutable. Runtime operations already protect against duplicate children and Stop-vs-Restart races, and exact child listener ownership is checked on readiness. `app.Run` currently aborts on `EnsureStarted` failure; that behavior must change for installed content.

## Goals / Non-Goals

**Goals:** Connect durable registry state to the existing single runtime and route unmodified Kiwix output through the shell origin; provide a small stable API allowing backend and UI work to remain disjoint.

**Non-Goals:** New renderer, HTML substitution, search normalization/UI (change 07), FTS, update/uninstall policy, polling the filesystem, external runtime attachment, generic plugin infrastructure or database schema changes.

## Decisions

### 1. Private installed snapshots and post-commit notification

Add a private application-facing installed snapshot carrying package ID and canonical owned ZIM path. Publish snapshots only after change 05 reconciliation/verification or a successful durable installation transaction. Use the package service's ownership checks and cheap file identity checks when deriving new input snapshots; never call its whole-file verify from status/proxy handlers. The public package API remains unchanged.

Add an optional successful-install callback that runs outside service/database locks after commit, only notifying a serialized application coordinator. It must not block installation on runtime startup or turn a runtime failure into a package persistence error. Startup explicitly reads the reconciled snapshot; callbacks missed during initialization are covered by reading the latest full set, not incremental append events. A coalesced notification re-reads the latest full trusted set so concurrent commits cannot drop a package. Stop and join this coordinator before closing package roots/database. Reject directory scanning and status-time full SHA-256 reads: both bypass the durable authority or make multi-GB content impractical.

### 2. Replace input sets through the existing lifecycle lock

Add the minimal concrete provider/runtime operation needed to apply a copied, sorted, deduplicated complete ZIM set. Serialize comparison, old-child cancellation/reaping, input replacement and new readiness using existing operation barriers and stop epoch. Snapshot launch inputs per attempt rather than mutate a slice while launch reads it. Equal ready sets are no-ops; failed equal sets are retried only explicitly; empty sets stop without binary resolution. New committed sets can trigger a fresh bounded attempt. Keep Stop's immediate cancellation and epoch invalidation: neither a queued refresh nor retry may resurrect a child after shutdown wins. Do not build a second process manager or invoke public Stop as an uncoordinated prelude to Restart.

### 3. Verified Kiwix URL-root contract

Official Kiwix 3.8.2 was exercised with the existing 259145-byte tiny ZIM, including a copy renamed to a 32-hex installed ID. Evidence: `/tmp/engelsiz-orchestration-2026-10-29/serving/probe.json` and `probe-renamed.json` (temporary verification evidence, not release assets).

Launch with `--address=127.0.0.1`, private port, **`--urlRootLocation=/content`**, then trusted ZIM path arguments. Readiness must probe `/content/`: it returns 200 whereas `/` returns 404 with this setting. Keep exact-child listener ownership checks before/after readiness. Preserve the full `/content/...` path upstream; do not StripPrefix.

Observed: `/content` redirects 302 to `/content/`; `/content/content/<filename-stem>/` redirects 302 to `/content/content/<stem>/Wikibooks.html`; article returns 200 (83784 bytes); nested `Wikibooks_files/Haskell-logo.png` returns 200 (2831 bytes). `/content/skin/*`, catalog, viewer and nojs resources carry the prefix. Kiwix chooses the installed basename stem as book key, so registry IDs safely determine open hrefs without parsing upstream catalog or exposing paths. Preserve Kiwix relative article resources; do not rewrite archived HTML. The sample includes external editorial hyperlinks, which are not required offline resources and do not justify a renderer rewrite.

### 4. Private owned proxy operation

Expose a private Go operation from runtime/provider to serve content or supply a generation-bound trusted endpoint; never add endpoint fields to public RuntimeStatus. At dispatch, require current Ready generation and live child. Bind transport connections to that attempt: after dialing but before sending HTTP, re-check the current generation, live child and positive exact-listener ownership evidence; close and fail if any check fails. An already established checked connection cannot migrate to a replacement listener. Fail closed if ownership cannot be established, and never hold lifecycle mutexes across streamed responses. A pre-dial check alone leaves a child-exit/port-reuse gap. Keep endpoint validation in the runtime boundary and close idle transports when replacing a generation. Proxy transport uses `Proxy:nil`, bounded connect/response-header timeouts and request context; no environment proxy or redirect-following client.

Register exact `/content` canonical redirect and `/content/` handler before SPA fallback; allow GET/HEAD and reject other methods with 405. Use standard reverse proxy with explicit fixed upstream URL/Host, preserving validated path/query. Strip untrusted Forwarded and X-Forwarded-* values. Reject absolute/network-path forms, backslashes and dot-segment/encoded traversal ambiguity without banning legitimate escaped article names. Query values never select targets. Validate Location by resolving against the fixed upstream: emit only `/content/`-confined paths (with query/fragment), remove internal authority, and reject foreign/escaping targets with sanitized 502. Filter other origin-bearing headers that could disclose private upstream endpoints; never return raw transport errors. Pre-stream failures render a small local 503/502 page with “İçerik şu anda açılamıyor.” and an `/` return link; midstream failures abort safely and log, without appending diagnostics to content.

### 5. Exact public API and UI ownership contract

Runtime ownership is `internal/runtime/**` and `internal/providers/**`; backend integration ownership is `internal/packages/**`, `internal/app/**` and `internal/api/**` (including their tests and routing); UI owns `frontend/src/**`; root integration owns embedded generated assets, shared planning/task completion and end-to-end verification. Backend must not edit frontend and UI must not edit Go files. No schema migration expected.

- `GET /api/runtime` → 200, JSON and `Cache-Control:no-store`:
  `{ "runtime": { "state": "ready", "error": null }, "contents": [{ "packageId": "<32hex-id>", "href": "/content/content/<32hex-id>/" }] }`.
- State union: `stopped | starting | ready | stopping | failed`. Error is null unless failed, then `{ "code":"runtime_unavailable", "message":"İçerik hizmeti başlatılamadı. Tekrar deneyin." }`. No PID, binary/path, internal address or technical LastError.
- `contents` is empty outside Ready and contains only the successfully loaded generation, never a newly installed but not yet loaded package.
- `POST /api/runtime/retry` accepts only `{}` (bounded body, no query parameters) → 202 same shape; schedules a bounded attempt from the latest installed snapshot, duplicate in-progress retries return 202. A healthy equal ready set is an accepted no-op. No installed inputs → 409 `{error:{code:"no_installed_content",message:"Henüz kurulu içerik yok."}}`; malformed input →400 `invalid_request`; storage failure →500 existing sanitized storage error. Other methods →405 with Allow. GET rejects unexpected query parameters. These routes never initiate downloads/catalog calls.
- UI module may export shared `RuntimeResponse` and a `useRuntime` hook. Installed view polls immediately and every 2 seconds while visible, one request at a time; abort on hide/unmount and resume on visibility. Retry disables while submitting/starting/stopping and updates snapshot. Continue polling after a ready state so child exits become visible. Show pending activation for installed IDs absent from contents. Show ready matching href as a normal “İçeriği aç” link (browser back returns to shell); do not synthesize upstream URLs. Use roles/status and accessible error/retry controls consistent with current package view.

### 6. Startup failure is a content failure

After registry reconciliation, load the trusted set and attempt startup before the existing HTTP health/browser sequence. Log ordinary runtime failure but continue to start HTTP/UI; application context cancellation still performs normal rollback. Runtime-independent database or HTTP binding failures retain existing fatal handling. Shutdown cancels and joins refresh work before roots/database close and reaps its child. Keep `/api/health` about shell readiness.

## Risks / Trade-offs

- [Controlled restart briefly interrupts content requests] → publish Starting/Stopping, empty links while unavailable, bounded recovery and retry; no zero-downtime dual children.
- [Ownership proof unavailable on a platform] → fail closed with friendly shell state; preserve Windows/Linux ownership code and cross-build checks.
- [Local files externally altered after startup verification] → private owned snapshots plus cheap identity checks at activation; no promise to rehash gigabytes per request.
- [Archived external editorial links] → preserve Kiwix behavior; verify archive-local required resources offline and document sample limitations.
- [Refresh races or shutdown regressions] → deterministic delayed-start tests and `go test -race` on lifecycle/coordinator paths, preserving existing Stop-vs-Restart cases.

## Migration Plan

No migration. Integrate backend/API first and UI against the fixed response contract, regenerate embedded frontend, then perform real-binary smoke in an isolated profile. Rollback removes serving/UI integration without deleting installed files or registry records. Release packaging/licensing remains the later release change; temporary official test binary is not committed.
