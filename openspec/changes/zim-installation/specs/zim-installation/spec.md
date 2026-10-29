## Purpose

Turn completed downloads into checksum-verified local ZIM installations with durable package state, safe recovery and an offline management interface.

## ADDED Requirements

### Requirement: Immutable verification metadata
The system SHALL persist provider/package identity, content type, language, variant, title, version, exact expected size, source URL and available supported SHA-256 from the selected acquisition alongside its download. Installation SHALL use that saved release metadata without fetching current catalog or Metalink data. Missing or unsupported checksum and legacy downloads without required acquisition metadata SHALL remain downloadable history but SHALL NOT be installed by inferring trust from their bytes or a later release.

#### Scenario: Catalog changes after acquisition
- **WHEN** a completed download is installed after the upstream catalog changes or becomes unavailable
- **THEN** installation uses its persisted release and checksum, without a network request.

#### Scenario: Unverifiable legacy artifact
- **WHEN** installation is requested for a completed download with missing acquisition metadata or no supported checksum
- **THEN** installation is rejected with a clear fresh-download or checksum-unavailable error and the file is not published as installed.

### Requirement: Verified installation boundary
The system SHALL explicitly install only a completed, application-owned regular `.part` file. It SHALL stream SHA-256 verification and compare exact byte size before atomically moving the file into OS user-data `content/kiwix` as a final `.zim`. It SHALL NOT overwrite another package, follow an untrusted path or expose unverified content as installed. Verification SHALL NOT prevent controlling other downloads.

#### Scenario: Matching bytes
- **WHEN** installation verifies the saved SHA-256 and exact size successfully
- **THEN** one final `.zim` and one installed package record exist, with the selected variant and version.

#### Scenario: Checksum or size mismatch
- **WHEN** owned completed bytes have the wrong hash or size
- **THEN** the package becomes failed with a readable verification error, retains its source for diagnosis or retry, and has no installed final file.

#### Scenario: Unsafe file or target collision
- **WHEN** a source escapes ownership, is a symlink or nonregular file, or an existing target belongs to another artifact
- **THEN** installation fails safely without overwriting, moving or publishing that file.

### Requirement: Durable package registry
The system SHALL persist package identity, download identity, provider, provider package ID, content type, language, variant, title, version, source URL, local path, size, checksum, status, structured error and created/updated/installed timestamps in SQLite metadata. Package states SHALL be verifying, installing, installed and failed. Installed time SHALL be null until successful installation. Repeated requests and retries SHALL reuse the same package identity rather than create duplicate rows. Download transfer state SHALL remain completed after its artifact is consumed by installation.

#### Scenario: Offline listing after restart
- **WHEN** the application reopens without internet after successful installation
- **THEN** the registry still lists its installed variant, version and timestamps, and download history remains completed despite the renamed `.part`.

### Requirement: Recoverable asynchronous installation
The application SHALL own accepted installation work independently of the HTTP request. It SHALL persist finalization intent before rename and reconcile interrupted verification/finalization against owned source and destination bytes before exposing installed status. Shutdown SHALL stop and join workers before database closure. Ambiguous or invalid files and persistence failures SHALL produce recoverable state without falsely claiming installation or deleting a different artifact.

#### Scenario: Interrupt before rename
- **WHEN** the application stops during verification or after finalization intent while the source `.part` remains
- **THEN** reopening safely re-verifies the source and either completes installation or records an explicit recoverable failure.

#### Scenario: Interrupt after rename
- **WHEN** rename succeeded but the installed transaction was interrupted
- **THEN** reopening verifies the intended owned final file, reconciles registry and consumed download ownership, and produces one installed package without falsely failing its completed transfer.

#### Scenario: Rename or persistence failure
- **WHEN** rename fails, or persistence fails before or after rename
- **THEN** no unverified installed state is returned and retained files plus durable intent permit safe recovery or an explicit retry.

### Requirement: Package HTTP contract
The system SHALL provide `GET /api/packages` returning HTTP 200 with `{packages: Package[]}` for all package states and optional `?status=installed` filtering. A Package SHALL contain `id`, `downloadId`, `provider`, `providerPackageId`, `contentType`, `language`, `variant`, `title`, `version`, `sizeBytes`, `checksum: {algorithm: "sha256", value: string}`, `status`, `error: {code, message} | null`, `createdAt`, `updatedAt` and `installedAt: string | null`. Responses and user errors SHALL omit internal paths, source URLs and raw storage errors.

`POST /api/downloads/{id}/install` SHALL accept an empty JSON object and return `{package: Package}`: HTTP 202 for accepted verification or an already active installation, HTTP 200 for the same already installed artifact, and HTTP 202 for an explicit failed-install retry. Missing downloads SHALL return 404; incomplete transfers, ownership conflicts and target collisions 409; unavailable supported checksum or missing legacy acquisition metadata 422; storage failure 500; malformed input or unsupported list filters 400. Errors SHALL use `{error: {code, message}}`. Concurrent duplicate requests SHALL start at most one worker for the artifact.

#### Scenario: Repeat accepted installation
- **WHEN** two requests install the same completed download concurrently and a later request follows completion
- **THEN** active requests return 202 with the same package ID and at most one worker, and the later request returns 200 with that installed package.

#### Scenario: List and reject invalid state
- **WHEN** a client lists packages offline and then requests installation of a paused download
- **THEN** listing returns the safe persisted snapshots and installation returns 409 with a readable error.

### Requirement: Package management interface
The existing UI SHALL offer installation for completed downloads, display verifying/installing/failed/installed states and eligible explicit retry, and show installed variant and version from the package API. It SHALL distinguish downloaded bytes from verified installed content and retain the application shell on failures. Turkish user messages, keyboard-accessible controls and status semantics SHALL be provided. Package polling SHALL avoid overlap and stop when hidden or unmounted. No installation success SHALL imply a serving refresh, search availability or update replacement.

#### Scenario: Install and revisit offline
- **WHEN** the user installs a completed download and later reopens the package view offline
- **THEN** verification and installation progress resolve to the persisted installed variant/version or a readable retryable failure, without requiring catalog access.
