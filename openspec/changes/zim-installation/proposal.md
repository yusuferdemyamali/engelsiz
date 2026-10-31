## Why

Change04 ends with an unverified `.part`; users cannot yet distinguish downloaded bytes from a trusted installed Wikipedia package. Add the verification and installation boundary required by main-spec AC-09 while preserving resumable transfer identities and offline metadata.

## What Changes

- Persist acquisition-time package metadata and supported SHA-256 from the exact selected Kiwix Metalink file alongside the download; never resolve a different release during installation.
- Add a `content_packages` registry containing provider/package identity, content type, language, variant, title, version, source URL, local path, size, checksum, status and timestamps.
- Explicitly install a completed download: verify its owned bytes, atomically rename `.part` into OS user-data `content/kiwix`, then mark installed. Persist intermediate intent and recover interruption without publishing unverified content.
- Provide safe package listing and asynchronous install APIs; show verification, failure, installed variant and version in the existing UI.
- Acceptance: matching SHA-256 and exact size produce one installed row/final `.zim`; wrong/missing/unsupported checksum never installs; rename/DB failures and interruption remain recoverable; retries do not duplicate rows or overwrite another package; offline listing and frontend errors expose no internal paths.
- Exclude update/replacement, serving refresh, search, custom ZIM parsing and Wikipedia-to-SQLite import.

## Capabilities

### New Capabilities

- `zim-installation`: Immutable acquisition handoff, verified atomic installation, durable package registry, recovery and package management UI/API.

### Modified Capabilities

None: prior capabilities currently live in unarchived change artifacts; this additive capability extends their handoff without editing those artifacts.

## Impact

Provider acquisition metadata; generic download descriptor persistence and completed-file ownership; additive SQLite migrations (both embedded and reference copies); a small installation service; API/app lifecycle wiring; React package/download views. Use Go standard-library SHA-256 and existing dependencies. Previously created downloads lacking the immutable verification metadata remain visible but require a fresh download for installation; do not infer their release from the current catalog.
