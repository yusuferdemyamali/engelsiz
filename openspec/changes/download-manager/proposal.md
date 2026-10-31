## Why

The catalog can identify Turkish Wikipedia variants, but users cannot yet transfer their large files or recover interrupted downloads. This change implements the resumable transfer portion of `docs/main-spec.md` §§11, 16–19 and AC-07/08/17 using the scope in `docs/changes/04-generic-resumable-download-manager.md`.

## What Changes

- Add a provider-independent HTTP/HTTPS streaming manager with SQLite persistence, `.part` files, byte progress, pause/resume, cancel and explicit retry.
- Preserve partial bytes across shutdown/crash and safely resume with HTTP Range; restart from zero when upstream cannot safely continue an existing entity.
- Check actual target-volume free space before writing and preserve partial data on runtime disk-full failures.
- Resolve catalog selections through the sole Kiwix provider, including bounded Metalink resolution into an actual ZIM URL, filename and precise size. Never download a `.meta4` document as the selected ZIM.
- Add download creation/list/control APIs and usable catalog download actions plus an accessible Downloads view.
- Transfer completion keeps the unverified `.part`; checksum verification, final installation/rename, serving refresh and updates remain outside this change.

## Capabilities

### New Capabilities

- `download-manager`: Generic persistent transfers, provider metadata handoff, safe restart/resume, disk handling and user-facing API/UI controls.

### Modified Capabilities

None. Earlier change artifacts describe their own implementation stages; existing catalog response fields remain compatible.

## Impact

Add `internal/download`, a downloads migration, API handlers, application lifecycle wiring and provider acquisition resolution. Extend existing React views/styles using existing dependencies. Use platform-specific free-space calls for Linux and Windows. No new provider system, scheduler framework or runtime installation is introduced.

Acceptance requires a deterministic local HTTP transfer fixture proving identical resumed bytes after manager/database reopen, safe 200 Range fallback and changed-entity handling, preflight low-space rejection without body writes, injected disk-full failure preserving a retryable partial file, persisted controls/progress, real Metalink-to-ZIM handoff and working UI actions. Validate Go tests/race coverage, Windows compilation and frontend production build; document a local UI smoke check.
