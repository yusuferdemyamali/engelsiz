## Purpose

Keep the offline knowledge workflow usable and local data intact through expected operational failures while providing bounded, private diagnostics.

## ADDED Requirements

### Requirement: Bounded private operational logs

The system SHALL log startup/shutdown, selected local ports, runtime start/stop and catalog, transfer, verification and database failures to files. Log retention SHALL be bounded through simple rotation. Logs SHALL NOT contain search queries, article bodies, credentials or unbounded upstream response bodies. User-visible failures SHALL use readable messages without raw stack traces or private filesystem paths.

#### Scenario: Log reaches retention threshold
- **WHEN** ordinary operation fills the current log
- **THEN** logging continues with bounded rotated history and restrictive file permissions

#### Scenario: Provider returns sensitive or oversized error data
- **WHEN** an upstream operation fails
- **THEN** diagnostics identify the failing operation without logging response bodies, credentials or user content, and the UI receives a stable error

### Requirement: Recoverable failures preserve local data

Offline/catalog failure, disk exhaustion, interrupted download, verification mismatch, runtime startup failure and port collision SHALL preserve verified content and partial download state where applicable, keep the shell usable where storage permits, and expose existing retry/resume controls. Database corruption or migration failure SHALL stop safely with diagnostics and SHALL NOT reset or delete the database automatically.

#### Scenario: Database cannot be opened
- **WHEN** SQLite reports corruption or migration failure
- **THEN** the original database remains intact, startup fails clearly and a bounded diagnostic is recorded

#### Scenario: Network or transfer fails
- **WHEN** catalog access fails or an in-progress download is interrupted
- **THEN** installed content remains available and retry/resume is offered without silently replacing it

### Requirement: Filesystem and process boundaries remain constrained

Untrusted metadata SHALL NOT escape owned download/content roots or create shell commands. Runtime launch SHALL use process APIs with distinct arguments. Public APIs SHALL omit internal paths and private runtime addresses. A failure-mode matrix SHALL map every failure in docs/changes/09 to implementation evidence and automated or manual validation.

#### Scenario: Hostile metadata path
- **WHEN** metadata contains traversal, absolute paths or platform-special filenames
- **THEN** acquisition/installation rejects it and no outside file is changed

#### Scenario: Runtime arguments contain special characters
- **WHEN** a trusted content path contains spaces or shell metacharacters
- **THEN** it is passed as one process argument without shell interpretation
