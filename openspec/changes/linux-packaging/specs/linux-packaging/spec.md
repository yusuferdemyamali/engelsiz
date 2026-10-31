## Purpose

Provide a verified, reviewable linux packaging path for the Engelsiz offline knowledge application.

## ADDED Requirements

### Requirement: Self-contained Linux archive
The release SHALL provide engelsiz-linux-x64.tar.gz containing engelsiz, runtime/kiwix-serve, required runtime dependencies, README and LICENSES. It SHALL run on the documented Linux x86_64 baseline without installing Go, Node, SQLite or Kiwix. Runtime resolution SHALL use the application executable location and data SHALL use XDG directories independently of cwd.

#### Scenario: Extract and launch from another directory
- **WHEN** a user extracts the package and launches its app by absolute path from a different working directory
- **THEN** the bundled runtime is found and user data is written to XDG directories

### Requirement: Verify native compatibility
Packaging SHALL reject a Linux app or runtime with an unbundled ELF interpreter/dependency. Supported baseline SHALL be Linux kernel 4.4 or newer, x86_64, with a desktop browser/opener; no host glibc version SHALL be required by the static app/runtime pair.

#### Scenario: Unexpected dynamic runtime
- **WHEN** an acquired runtime includes an ELF interpreter or shared-library requirement
- **THEN** packaging fails rather than silently requiring users to install missing libraries

### Requirement: Verified lawful runtime delivery
Packaging SHALL verify immutable runtime SHA-256 pins and require a reviewed source/license inventory covering the exact binary and incorporated dependencies plus the project's approved license. Missing hashes, notices, source payloads or license decisions SHALL fail closed before a distributable artifact is produced.

#### Scenario: Missing corresponding source
- **WHEN** a runtime archive is present but its complete reviewed corresponding-source bundle is missing
- **THEN** packaging fails with a precise actionable error and does not produce a release artifact
