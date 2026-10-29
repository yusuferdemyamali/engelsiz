## Context

See proposal.md for motivation. Existing SQLite migrations are embedded and use one connection; `AppDirs.DownloadDir` already exists. `ContentProvider` supplies catalog/runtime operations. Catalog acquisition has `kind: metalink|zim`; real `.zim.meta4` URLs are not transfer URLs and catalog sizes are approximate. Existing React `App.tsx` renders the shell/catalog and has no routing framework. Main specs are not yet archived; preserve prior change artifacts.

## Goals / Non-Goals

**Goals:** A small durable transfer service, provider-side acquisition resolution and a usable catalog-to-download flow, with deterministic failure tests.

**Non-Goals:** Checksums, final rename/install, serving refresh, update orchestration, mirrors switching, automatic retries, new dependencies or a general job/plugin framework.

## Decisions

### Provider boundary

Add a narrow provider acquisition resolution operation taking `providerPackageId` (extend the existing provider boundary or use a small capability interface). The server accepts only provider `kiwix` and a selected ID; it refetches/looks up the actual catalog package rather than trusting browser metadata. The Kiwix provider resolves direct acquisitions or bounded Metalink XML (15s metadata timeout, limited redirects/body, e.g. 1 MiB); match the expected file, require a safe single basename and positive Metalink size, choose the first acceptable HTTP/HTTPS URL deterministically. Production metadata URLs remain HTTPS, with existing injected local HTTP fixtures permitted for tests. Reject missing/ambiguous/unsafe metadata. Do not guess by stripping `.meta4`, use catalog length as exact transfer size, or implement mirror switching. Hashes are not verified in04.

Pass a generic transfer descriptor with provider/package identity, title, variant, filename, source URL and optional exact total to `internal/download`. The descriptor itself contains no Kiwix behavior. Direct-file unknown length can be established from HTTP metadata; a HEAD failure alone must not prevent a valid GET. Use separate metadata and long-lived streaming HTTP settings so the catalog's 15s timeout cannot kill large file bodies.

### Durable transfer ownership

Add `002_downloads.sql`: ID, provider, provider_package_id, title, variant, file_name, source_url, owned temp_path, nullable total_bytes, downloaded_bytes, status, optional ETag/Last-Modified, error_code/message, created_at/updated_at. Derive an ID-isolated `.part` path under DownloadDir; validate provider basenames against slash/backslash/absolute/traversal names on both OSes and reject symlinks/unsafe persisted paths before opening/removing files. Browser clients never select paths. Use conditional state updates plus per-transfer worker ownership so duplicate controls cannot create two writers. Keep concurrency simple: one sequential worker, queued records in SQLite, no priority/retry scheduler.

Checkpoint counters periodically (about once per second) and on transitions. Close/sync files before terminal persistence. On startup stat owned partial files, reconcile actual length, turn queued/downloading records into paused and do not auto-transfer. Missing partials reconcile to zero; inconsistent/unsafe files fail clearly. Read and close SQL rows before issuing writes because the DB has one connection. Preserve failed/paused data; cancellation waits for its worker to exit before removing the owned partial. Shutdown rejects new work, cancels and joins workers, persists paused state, then application closes SQLite. A failed persistence operation must not masquerade as a successful control/completion.

### Safe transfer and disk behavior

Stream with bounded buffers, request identity encoding, validate statuses/lengths and handle short writes. For offset >0, use Range and If-Range with a strong ETag or usable Last-Modified. Without a usable validator restart from zero. Append only validated 206 with matching start/total and consistent validator; reject invalid/unsolicited ranges. A 200 response means restart: perform full-size space check before truncating old bytes. Changed validator/total or 416 triggers at most one fresh full request, never concatenation or an infinite retry. Preserve partial bytes until a valid restart response and disk check succeed. Known total requires exact successful length; unknown total becomes final actual size only at successful EOF.

Use Linux Statfs and Windows GetDiskFreeSpaceEx (already available x/sys) behind a small injectable capacity function. Compare actual free bytes with required remaining bytes; a restart needs full size. Fail closed if the capacity call fails. Unknown size checks available capacity but cannot guarantee the full transfer will fit. Inject file writing and capacity at the service boundary for deterministic ENOSPC tests; actual write failures preserve actual file length and expose failed/retry state. Explicit retry uses the same safe continuation algorithm and rechecks disk.

### API contract (frontend implementation contract)

All endpoints are same-origin JSON with no-store responses. Public `Download` is:

```ts
interface Download {
  id: string; provider: string; providerPackageId: string;
  title: string; variant: string; fileName: string;
  status: "queued" | "downloading" | "paused" | "failed" | "cancelled" | "completed";
  downloadedBytes: number; totalBytes: number | null; percentage: number | null;
  error: { code: string; message: string } | null;
  createdAt: string; updatedAt: string;
}
```

- `POST /api/downloads`, body `{provider:"kiwix", providerPackageId:string}` → `201 {download: Download}` after trusted resolution and initial disk check; enqueue/start. Reject duplicate unfinished selection with409.
- `GET /api/downloads` → `200 {downloads: Download[]}` newest first, including persisted history, empty array rather than null.
- `GET /api/downloads/{id}` → `200 {download: Download}`.
- `POST /api/downloads/{id}/pause|resume|cancel|retry` → `200 {download: Download}` after persisted action.
- Pause: queued/downloading→paused; resume: paused→queued; retry: failed→queued; cancel: queued/downloading/paused/failed→cancelled; repeated pause/resume-on-active/cancel are idempotent. Completed/cancelled cannot resume/retry. Conflict409 for other transitions; missing404, malformed/unsupported input400, unsupported method405 with Allow, provider resolution502, capacity preflight507, unexpected storage failure500.
- Errors: `{error:{code:string,message:string}}`, Turkish safe message; use codes such as `invalid_request`, `not_found`, `invalid_state`, `download_exists`, `source_unavailable`, `insufficient_space`, `storage_error`. Runtime failure appears in the Download snapshot. Never expose transfer URLs, validators, full paths or raw upstream error bodies.

### UI and work separation

Frontend worker owns `frontend/src/App.tsx`, styles and any new frontend download component; backend worker owns Go code/migrations/tests/app wiring. Share only the contract above. Add catalog “İndir”, pending/error state and an “İndirmeler” view/section within the current shell. Show filename/title/variant, localized bytes, unknown-total text, progress semantics, status and eligible Turkish controls. “İndirme tamamlandı; doğrulama ve kurulum bekleniyor” communicates transfer-only completion. Load persisted history on entry; poll around1s while the visible view has active downloads, with abort/visibility/unmount cleanup and no overlapping requests. No polling when hidden; refresh once on return/actions. Retain catalog error recovery and current design tokens; no navigation framework.

## Risks / Trade-offs

- [Range or validator unavailable] → Safe full restart can use more bandwidth; do not silently append.
- [Free space changes after check] → Runtime write failure retains `.part`; deterministic injected ENOSPC test proves retry behavior.
- [Crash between write and checkpoint] → Recovery uses validated owned file length, not a stale database counter.
- [Transfer completion is unverified] → Keep `.part` and explicit UI wording; later change owns checksum/install.

## Migration Plan

Apply the additive migration through existing startup migration machinery, then initialize recovery before serving API. Existing settings/catalog/runtime behavior stays compatible. Reverting application code can leave the unused downloads table and partial files intact; never reset the database or delete existing content. Verify local httptest transfers (including manager and DB reopen), race checks, Windows compilation, frontend build and a controlled tiny-file browser flow; no live multi-gigabyte download is required.
