## Why

Engelsiz needs a durable, local map archive that can be viewed after connectivity is lost. This change extends the v0.1 scope in `docs/main-spec.md`, which currently excludes maps, at the user's explicit request.

## What Changes

- Add a map archive and viewer, initially offering Eskişehir province through Z15.
- Extract a region from a configurable, trusted Protomaps PMTiles v3 source without downloading the planet.
- Persist package metadata in SQLite and serve installed files through a loopback HTTP Range endpoint.
- Bundle all renderer, style, glyph and sprite resources locally and credit OpenStreetMap and Protomaps.
- Preserve Wikipedia, generic downloads, application lifecycle and platform packaging.

## Capabilities

### New Capabilities

- `offline-maps`: Regional map acquisition, installation, persistence, deletion and offline viewing.

### Modified Capabilities

None.

## Impact

Backend provider, persistence, application wiring and distribution; React archive and viewer; local map assets. Navigation, geocoding, GPS, routing and dynamic regions remain out of scope.
