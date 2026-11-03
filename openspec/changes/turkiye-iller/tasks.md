## 1. Catalog data

- [x] Generate the 81-province catalog from `/tmp/opencode/iller-bbox/iller-bbox.json` with OSM relation provenance; Eskişehir keeps id `eskisehir` and its exact bbox.
- [x] Replace the hardcoded single entry with the generated catalog; no migration change (table already keyed by package id).

## 2. Backend and compatibility

- [x] Keep lifecycle, cancellation, Range service and recovery identical for every province; existing maps tests still pass.
- [x] Verify upgrade with a pre-installed Eskişehir archive keeps `ready` without re-download.

## 3. Picker UI

- [x] Province picker: Turkish-alphabetical list, name search, per-province status; existing viewer and controls unchanged.
- [x] Narrow-viewport check of the picker and viewer.

## 4. Verification

- [x] `go test ./...`, frontend tests/build, `npm audit`, Python packaging tests.
- [x] Real acquisition + offline viewing for small, large and metropolitan sample provinces; record archive hashes.
- [x] Update `verification.md` with catalog provenance and sample evidence.
