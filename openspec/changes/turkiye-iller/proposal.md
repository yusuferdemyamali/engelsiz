## Why

Engelsiz's map archive currently offers only Eskişehir. Users want all of
Türkiye available offline, choosing which provinces to download. Downloading a
single whole-country extract would be gigabytes and wasteful; per-province
packages match the existing regional-extraction design and let users pick only
what they need.

## What Changes

- Replace the single-province catalog with all 81 Turkish provinces, each a
  separately downloadable Z0–Z15 regional package from the same trusted
  Protomaps PMTiles v3 source.
- Province bounding boxes are resolved once from OpenStreetMap through
  Nominatim, recorded with relation identity and lookup date, and committed
  as a generated catalog. No user-supplied URLs or coordinates.
- Add a province picker to the Haritalar view: Turkish-alphabetical list with
  search, per-province status, and the existing download/cancel/retry/delete
  and viewer flows unchanged.
- Eskişehir keeps its existing package identity so already-installed archives
  survive the upgrade.

## Capabilities

### New Capabilities

- `turkiye-iller`: All-province offline map catalog, per-province acquisition
  and viewing.

### Modified Capabilities

- `offline-maps`: Single-province catalog becomes the 81-province catalog; all
  lifecycle, Range-service and offline-viewing behavior is preserved.

## Impact

Backend provider catalog (generated data, no schema change expected),
SQLite state reuse, React archive picker UI, map asset pipeline unchanged.
Navigation, geocoding, GPS and routing remain out of scope.
