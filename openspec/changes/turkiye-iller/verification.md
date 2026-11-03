## Catalog provenance, 2026-11-03

- 81 province bounding boxes collected from Nominatim (`state=<il>`,
  `country=Türkiye`, 1 req/1.2 s, 81 requests, no fallback needed). Every
  record is `osm_type=relation`, `class=boundary`, `type=administrative`.
  Raw data: `/tmp/opencode/iller-bbox/iller-bbox.json`; report:
  `/tmp/opencode/iller-bbox/iller-bbox-raporu.md`.
- Eskişehir record matches the previously established bbox exactly
  (`29.9883672,39.0349197,32.0115423,40.1872590`, zero deviation).
- `internal/providers/maps/catalog.go` generated from that JSON (IDs are
  ASCII slugs, e.g. `istanbul`, `sanliurfa`; all 81 unique and ASCII).
  Eskişehir keeps id `eskisehir` and its exact bbox, so installed archives
  survive the upgrade.
- Two generator bugs caught by tests and fixed before commit: lowercase-`İ`
  produced `i̇stanbul` (translate-then-lower order), and uppercase `Ç/Ş`
  produced non-ASCII slugs (missing uppercase entries in the map).

## Backend compatibility, 2026-11-03

- No migration change; `map_packages` already keyed by package id. Lifecycle,
  cancellation, Range service and recovery code untouched.
- Existing maps tests updated only where they assumed a single-entry catalog
  (Eskişehir lookup helpers, 81-count assertions). `go test -count=1
  ./internal/providers/maps/` passes; full `go test ./...` passes (11
  packages ok), `go vet` clean.
- Upgrade check: dev database with pre-installed Eskişehir `ready` restarted
  under the 81-province catalog — Eskişehir stayed `ready`, no re-download.

## Picker UI, 2026-11-03

- `frontend/src/maps.tsx` only: `İl ara` search (`role="search"`, labelled),
  `localeCompare('tr')` ordering, `toLocaleLowerCase("tr-TR")` filtering,
  result counter (`role="status"`), empty-result message; per-province status
  badges and existing download/cancel/retry/delete/viewer flows unchanged.
- `npm run build` passes; 390 px narrow check shows zero horizontal overflow.
- Screenshots: `/tmp/opencode/iller-picker/` (`maps-genis-liste.png`,
  `maps-genis-filtreli.png`, `maps-dar-liste.png`, `maps-dar-filtreli.png`,
  `maps-viewer-eskisehir.png`, `istanbul-viewer.png`).

## Representative real acquisition, 2026-11-03

- Via the app API against the trusted source: Yalova → `ready`, 7,273,835
  bytes (`80711a97…`); İstanbul → `ready`, 79,447,437 bytes (`794faa6b…`).
- Both archives pass `pmtiles verify`; İstanbul header reports MVT, Z0–Z15,
  requested bounds. `/api/maps/istanbul/file` serves 206 with correct range.
- İstanbul viewer renders: Bosphorus strait, Black Sea, Marmara Denizi,
  Turkish district labels, road shields, full-canvas fill, attribution
  `© OpenStreetMap · Protomaps`, zero page errors. (First screenshot
  mistakenly captured Eskişehir — harness clicked the first ready button;
  fixed by filtering to İstanbul first.)
- `node --test frontend/tests/*.test.mjs`: 10/10. `python -m unittest
  discover -s scripts/tests`: 17/17. `npm audit`: 0 vulnerabilities.
