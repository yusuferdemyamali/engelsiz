## Purpose

Enable reliable provider-independent large-file transfers with persistent progress, safe interruption recovery and user-controlled download actions.

## ADDED Requirements

### Requirement: Trusted provider metadata handoff
The system SHALL resolve a selected provider package into an actual HTTP/HTTPS file URL, safe filename and expected transfer size before creating its transfer. The generic transfer layer SHALL contain no Kiwix, Wikipedia or variant-specific URL/business logic. A Kiwix Metalink acquisition SHALL be resolved by the provider; its document SHALL NOT be transferred as the ZIM. Unsupported or malformed acquisition data SHALL return a recoverable error without starting a file transfer.

#### Scenario: Catalog Metalink selection
- **WHEN** the user starts a catalog package whose acquisition is a Metalink
- **THEN** the provider resolves the actual file URL and precise file size and the manager transfers that file rather than the Metalink document.

#### Scenario: Untrusted creation input
- **WHEN** a client attempts to supply arbitrary source URLs or destination paths instead of a supported provider/package selection
- **THEN** creation is rejected and no file is opened outside the application downloads directory.

### Requirement: Streaming partial file and truthful completion
The system SHALL stream response bytes to a uniquely owned `.part` under its downloads directory, exposing downloaded bytes, nullable total bytes and nullable percentage. A completed transfer SHALL retain its unverified `.part`, SHALL NOT become installed or served, and SHALL NOT claim checksum verification. Known-size truncation or overflow SHALL fail rather than report completion. Unknown totals SHALL remain indeterminate until successful EOF establishes the total.

#### Scenario: Completed transfer remains unverified
- **WHEN** the complete expected payload has been written successfully
- **THEN** status is completed, byte progress matches the payload, the `.part` remains and no package installation or runtime refresh occurs.

### Requirement: Persistent recovery and explicit controls
The system SHALL persist transfer identity, source and file metadata, counters, state, errors and timestamps in SQLite. Supported states SHALL be queued, downloading, paused, failed, cancelled and completed. Pause and shutdown SHALL stop transfer writes, close handles and preserve partial bytes. Restart SHALL reconcile the owned partial file and persisted state, expose interrupted active transfers as paused and wait for user resume. Resume SHALL continue paused transfers; retry SHALL attempt failed transfers using preserved safe partial data. Cancel SHALL stop the worker, remove its owned partial file and preserve a cancelled history record. Repeated same-state controls SHALL be safe; invalid transitions SHALL return a conflict. Concurrent controls SHALL NOT permit multiple writers for one transfer.

#### Scenario: Reopen after interruption
- **WHEN** a transfer is interrupted and the application/database is reopened with its partial file present
- **THEN** it is listed as paused with reconciled bytes, makes no automatic network transfer and can resume without losing valid partial bytes.

#### Scenario: Pause retry and cancel
- **WHEN** the user pauses an active transfer, resumes it, later retries a failed attempt or cancels it
- **THEN** each valid action has persisted observable state and completed cancellation leaves no worker writing or partial file behind.

### Requirement: Safe HTTP Range continuation
The system SHALL only append a response when HTTP 206 Content-Range starts at the local offset and its entity metadata/total are consistent. Resume SHALL use a saved usable entity validator when available. If the server returns a full 200 response, a changed entity or no usable validator, the system SHALL safely restart from zero after a full-size disk precheck rather than concatenate different payloads. Invalid ranges, unexpected encoding and ambiguous 416 responses SHALL never cause corrupt completion; they SHALL fail recoverably or restart safely. Partial bytes SHALL be retained until a restart response and its precheck are accepted.

#### Scenario: Range resume
- **WHEN** a server returns a validated 206 for the preserved byte offset
- **THEN** only the remaining bytes are appended and the final file exactly matches the fixture payload.

#### Scenario: Range ignored or entity changed
- **WHEN** resume receives 200 or discovers the source entity has changed
- **THEN** the transfer restarts safely from zero with sufficient space and never appends a full or changed response to old bytes.

### Requirement: Disk preflight and recoverable write failures
The system SHALL check available space on the destination volume before starting or continuing body writes, using the remaining expected bytes for valid resume and the full expected bytes for restart. Insufficient space or inability to inspect capacity SHALL prevent body writes and expose a clear error. If size cannot be established from provider or response metadata, the system SHALL check available space and stream with an indeterminate total while treating runtime write failures as recoverable. Runtime disk-full failure SHALL stop the worker, preserve actual written bytes and its `.part`, and permit retry after space is freed.

#### Scenario: Preflight low space
- **WHEN** available space is less than the required remaining transfer size
- **THEN** no body bytes are written and a disk-space error is returned or persisted for display.

#### Scenario: Disk fills during transfer
- **WHEN** a write fails because the destination fills after preflight
- **THEN** status becomes failed, actual partial bytes remain and retry can resume safely after capacity is restored.

### Requirement: Download HTTP contract
The system SHALL offer POST /api/downloads accepting provider and providerPackageId, GET /api/downloads returning a downloads array, GET /api/downloads/{id} returning a download snapshot and POST /api/downloads/{id}/{pause|resume|cancel|retry}. Creation SHALL return 201 with a download snapshot after provider resolution and preflight; repeated creation for a noncancelled/noncompleted matching selection SHALL return 409. Controls SHALL return 200 with a snapshot, missing records 404, invalid requests 400, invalid transitions 409, provider-resolution failure 502 and preflight capacity failure 507. Errors SHALL contain error.code and a user-readable error.message. Public snapshots SHALL omit source URLs, validators and internal filesystem paths.

#### Scenario: List and control a created download
- **WHEN** a valid package selection is created, listed and paused using the API
- **THEN** the API returns its stable ID, display metadata, counters and persisted status through the specified JSON contract.

### Requirement: Usable download screen
The catalog SHALL provide a working download action for available packages and the application SHALL show file/package, variant, status, downloaded/total bytes, determinate or indeterminate progress and eligible pause/resume/cancel/retry actions. Errors SHALL be understandable in Turkish and recoverable without replacing the application shell. Controls SHALL be keyboard accessible and progress/status changes SHALL have appropriate semantics. Completed copy SHALL explain that transfer completion does not imply verification/installation. Polling SHALL stop when the view is hidden/unmounted and SHALL avoid overlapping requests.

#### Scenario: Download and recover through UI
- **WHEN** the user starts an available catalog package, pauses it, reopens the Downloads view and resumes it
- **THEN** persisted progress and eligible controls are shown and the resumed transfer updates without refreshing the entire app.
