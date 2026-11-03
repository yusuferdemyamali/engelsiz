## ADDED Requirements

### Requirement: All-province catalog
The system SHALL offer all 81 Turkish provinces as separately downloadable
Z0–Z15 regional extractions from the configured trusted PMTiles v3 source,
with committed bounding boxes recorded from OpenStreetMap relations.

#### Scenario: Catalog listing
- **WHEN** the archive is listed with no prior downloads
- **THEN** all 81 provinces appear as `available` in Turkish alphabetical
  order with their region bounds and version.

#### Scenario: Existing installation preserved
- **WHEN** a user upgrades with an installed Eskişehir package
- **THEN** the Eskişehir package keeps its identity, file and `ready` state;
  no re-download is required.

### Requirement: Province picker
The system SHALL let the user find a province by Turkish name search and see
real per-province acquisition states, reusing the existing
download/cancel/retry/delete and viewer flows without fabricated progress.

#### Scenario: Search and acquire
- **WHEN** a user types a province name fragment
- **THEN** matching provinces filter in Turkish collation order and the
  selected province downloads, installs and opens exactly like a current
  single-province package.

### Requirement: Unchanged lifecycle and distribution
Regional extraction, cancellation, cleanup, atomic install, HTTP Range
service, local glyph/sprite/style rendering and release compliance handling
SHALL behave identically for every province; the change adds catalog entries
and UI, not new transfer or packaging mechanisms.

#### Scenario: Representative verification
- **WHEN** small, large and metropolitan provinces are acquired and viewed
  offline
- **THEN** install, restart persistence, Range behavior and local-only
  requests hold as established for Eskişehir.
