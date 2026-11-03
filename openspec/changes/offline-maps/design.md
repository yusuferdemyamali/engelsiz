## Context

The current download manager persists sequential HTTP file transfers. PMTiles regional extraction instead issues multiple Range reads and assembles a new archive. Treating the planet URL as a normal download would download the whole world and is prohibited. Extraction needs a provider-owned operation with lifecycle integration that leaves existing transfer behavior intact.

## Decisions

- Use the official version-pinned Go PMTiles extractor or a bundled version-pinned official CLI. The Windows and Linux release paths must include it; never rely on a system installation.
- The default source is `https://build.protomaps.com/20261102.pmtiles`, supplied by trusted provider configuration, not user request parameters. Range support and PMTiles header zoom bounds must be checked.
- The Eskişehir province bounding box is derived from OpenStreetMap relation 223421 (Nominatim response, 2026-11-02): west 29.9883672, south 39.0349197, east 32.0115423, north 40.1872590. Record source and version alongside catalog metadata. A bounding box includes surrounding areas.
- Store data in the application's data root, metadata and operation state in SQLite. Write a temporary file, verify the archive, then atomically install. Reconcile interrupted work at startup and clean failed or cancelled output.
- Serve only installed catalog identifiers via `http.ServeContent` or equivalent; never accept arbitrary filesystem paths. Enforce loopback and existing API protections.
- Bundle MapLibre, PMTiles protocol, compatible Protomaps v4 styling, glyph ranges and sprites. Verify actual network requests offline rather than assuming local availability.

## Risks

The existing generic manager cannot safely resume a partially assembled regional extract without additional integration. Cancellation must stop all workers before cleanup. A successful PMTiles header check alone does not prove that all tile data is intact. Local font coverage and style schema require browser verification. The dated source can expire and requires manual configuration replacement.
