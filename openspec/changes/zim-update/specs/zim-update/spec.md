## Purpose

Let users discover newer Wikipedia releases and deliberately update them while preserving a verified, usable offline version through transfer, verification, activation and application restart failures.

## ADDED Requirements

### Requirement: Compare compatible releases without automatic downloads

The system SHALL compare the committed installed release with upstream catalog releases only within the same provider, content type, language and variant. It SHALL produce `update_available` only for a strictly newer, parseable publication version with a different upstream release identity. Equal, older, missing or unparseable versions SHALL NOT be advertised as an update. Catalog checks and page loading SHALL NOT start downloads or replace content.

#### Scenario: A compatible newer release exists
- **WHEN** a newer Turkish Wikipedia nopic release is present for an installed nopic package
- **THEN** the update view reports `update_available`, the installed version, candidate version and candidate size from upstream metadata

#### Scenario: Incompatible or uncertain release
- **WHEN** the catalog contains only another variant/language, an equal or older release, or a release whose date cannot be compared
- **THEN** the package is not advertised as having an available update and no download starts

### Requirement: Explicit update action and transfer reuse

The user SHALL explicitly select `Güncelle` to create an update download. The server SHALL resolve trusted metadata and revalidate the selected upstream identity as newer and compatible with the still-active installed package; browser-supplied URLs, filenames and filesystem paths SHALL NOT be accepted. Transfers SHALL reuse existing progress, disk-space checks, `.part`, pause/resume, cancel and retry controls. Existing explicit verification/install controls SHALL remain available for completed update downloads. Conflicting operations for the same active package SHALL be rejected without creating another transfer.

#### Scenario: Confirmed update download
- **WHEN** the user chooses a displayed candidate
- **THEN** one associated generic download is created, progress is visible, and the existing version remains readable/searchable during download and verification

#### Scenario: Stale or duplicate action
- **WHEN** the user submits a stale candidate, a mismatched package, or an update already in progress
- **THEN** the action returns an explicit recoverable error and does not replace the active release or create a duplicate transfer

### Requirement: Candidate activation and durable commit

The system SHALL preserve the previous active selection until the new file is fully downloaded, exact-size and SHA-256 verified, atomically installed, and proven usable by the owned Kiwix runtime. Runtime readiness alone SHALL NOT establish candidate usability: an actual candidate-specific book/content request SHALL succeed through the owned generation-bound transport. The committed selection SHALL replace only the matching release, preserve unrelated active packages, and survive restart. A short unavailable period during the single-child activation transition SHALL be reported as loading rather than success. Old files SHALL remain retained after successful activation.

#### Scenario: Successful candidate activation
- **WHEN** the completed candidate passes verification, installation and its own Kiwix content usability check
- **THEN** it becomes the committed active release, its search/read links work, unrelated variants remain active, and the older file remains retained

#### Scenario: Runtime ignores an invalid candidate
- **WHEN** Kiwix starts and its general homepage is ready but it cannot serve the candidate book
- **THEN** the update is not committed or reported as successful

### Requirement: Failed update preservation and rollback

Download interruption, insufficient space, hash/size failure, installation failure and candidate activation/usability failure SHALL NOT delete or mutate the previous working package. Transfer and verification failure SHALL leave its serving runtime unchanged. Activation failure SHALL restore the entire previously committed serving selection and expose a user-readable update error. If restoration itself fails, the prior selection/files SHALL remain committed and retryable, with an explicit runtime failure rather than a false success. Cancellation and shutdown SHALL NOT restart children after shutdown or cause a candidate to become active unintentionally.

#### Scenario: Failed transfer and checksum mismatch
- **WHEN** an update transfer fails or its completed file fails exact-size or SHA-256 verification
- **THEN** the old file, registry identity and active selection are unchanged, and the user can still search/read the old version and recover the failed operation

#### Scenario: Candidate runtime fails
- **WHEN** a verified candidate fails startup or its candidate-specific usability check
- **THEN** the previous serving selection is restored, the update error remains visible, the candidate is excluded from active content, and the old version remains usable

### Requirement: Restart-safe update state

The committed active selection and candidate association SHALL be durable independently of file verification/install status. Cold startup SHALL NOT serve an uncommitted candidate merely because its file is verified or installed. Interrupted activation SHALL restore the previous selection without downloading or automatically attempting that candidate again. After successful commit a cold offline restart SHALL select the new version. Retained older versions SHALL NOT be mistaken for available updates or duplicated active content.

#### Scenario: Restart before activation commit
- **WHEN** the application stops after candidate installation or during activation before a successful durable commit
- **THEN** an offline restart serves the previous committed release, retains both files, and reports the interrupted operation as recoverable

#### Scenario: Restart after success or failure
- **WHEN** the application restarts offline after an update succeeded or candidate activation failed
- **THEN** it serves respectively the committed new release or preserved old release and local search/article access works without the catalog

### Requirement: Accessible update and failure presentation

The package UI SHALL show installed/new versions, upstream size (or an explicit unavailable-size label), an explicit update action, current transfer/verification/activation state, and retained-old-version or failure information. Controls SHALL be keyboard operable and asynchronous errors/statuses announced. Catalog failure SHALL preserve locally stored operation details, installed listing, search and reading; the UI SHALL distinguish unavailable comparison from no update. Browser requests SHALL use the Engelsiz origin and SHALL NOT expose private runtime ports or filesystem paths.

#### Scenario: Catalog unavailable during failed update recovery
- **WHEN** upstream catalog access fails after a local update attempt
- **THEN** the UI reports the unavailable check and retains the local operation/error and access to installed content and existing recovery controls
