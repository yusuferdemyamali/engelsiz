## Why

Verified Wikipedia packages can already be installed and served, but Engelsiz cannot yet search them or retain its navigation while reading. Complete the local reading flow required by `docs/main-spec.md` §§14–15 and AC-10/11/16 without depending on the online catalog.

## What Changes

- Add bounded local `GET /api/search` through KiwixProvider and the owned generation transport, normalizing Kiwix RSS results into plain text and safe same-origin article links.
- Add Turkish search UI with installed-package selection, loading, initial empty, no-results and recoverable error states.
- Open results using the existing Kiwix renderer inside an Engelsiz reading shell with an explicit return to search/navigation.
- Verify cold startup, installed packages, positive search and article assets in an isolated loopback-only network namespace; separately verify browser behavior with catalog unavailable.
- Reuse serving/lifecycle from change 06. No custom index, SQLite FTS, Wikipedia parser/renderer, AI, whole-Wikipedia mirroring, public runtime port, or arbitrary upstream target.

## Capabilities

### New Capabilities

- `offline-wikipedia`: Search installed local content through Kiwix and read articles with Engelsiz navigation, independent of catalog connectivity.

### Modified Capabilities

None. Earlier change artifacts remain the reference for lifecycle, package verification and `/content/*` proxy behavior.

## Impact

Go provider/search API and application wiring; React search/reading views and navigation; bounded fixtures and offline acceptance evidence. No database migration, new online service or production dependency is expected. Production catalog filtering remains Turkish Wikipedia; the tiny English upstream fixture proves the transport and rendering mechanism, not Turkish linguistic quality or full-archive performance.
