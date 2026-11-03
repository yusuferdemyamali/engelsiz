## ADDED Requirements

### Requirement: Regional acquisition
The system SHALL offer Eskişehir province as a Z0–Z15 regional extraction from a configured trusted PMTiles v3 source without downloading the complete planet.

#### Scenario: Successful installation
- **WHEN** a user starts acquisition and extraction and verification succeed
- **THEN** the system atomically installs the package and persists its metadata for restart.

#### Scenario: Failure or cancellation
- **WHEN** acquisition fails or is cancelled
- **THEN** the system removes incomplete output and never presents it as ready; duplicate concurrent acquisition is rejected.

### Requirement: Local archive service
The system SHALL serve only installed map packages from the existing loopback API and support HTTP byte Range requests.

#### Scenario: Byte range
- **WHEN** a valid or unsatisfiable Range request is made
- **THEN** the service responds with correct 206 or 416 semantics and headers.

### Requirement: Offline viewing
The system SHALL display installed maps with local MapLibre, PMTiles, Protomaps v4 style, glyphs and sprites, including Turkish labels and source attribution, without external runtime requests.

#### Scenario: Offline restart
- **WHEN** an installed map is opened after restart with internet disabled
- **THEN** pan and zoom across the region work with labels and icons and no external HTTP requests.

### Requirement: Package management
The system SHALL show real acquisition and installation states and allow deletion and retry without fabricated progress.

#### Scenario: Delete package
- **WHEN** the user deletes an installed package
- **THEN** its file and metadata are removed consistently.
