## 1. Source and design

- [x] Check the initial source URL, byte Range support and PMTiles header zoom levels.
- [x] Identify an OSM province boundary and record its bounding box and provenance.
- [x] Integrate the regional operation with the existing download lifecycle without altering Kiwix transfers.

## 2. Backend

- [x] Add trusted source configuration, catalog, SQLite state and recovery.
- [x] Integrate official extraction, cancellation, cleanup and verified atomic installation.
- [x] Serve installed files with HTTP Range and implement consistent deletion.
- [x] Test failure, cancellation, duplicate operations, restart, deletion and 206/416 behavior.

## 3. Frontend and distribution

- [x] Build archive and viewer using MapLibre, PMTiles and Protomaps v4 styles.
- [x] Bundle and verify local glyphs, sprites, Turkish labels and attribution.
- [x] Package the extraction component and assets for Linux and Windows.
- [x] Run existing and new tests, builds and real Z15 extraction.
- [x] Restart fully offline and verify rendering, interaction and absence of external requests.

Committed route records: `third_party/runtime-source-candidates/offline-maps/`
(candidate locks, refreshed compliance manifests, route README). Ignored build
output: `dist/offline-maps-package/` (candidate bundles, release tarball and
installer with checksums). Clean native Windows 10/11 acceptance remains
pending per `docs/release-validation.md` and is not claimed here.
