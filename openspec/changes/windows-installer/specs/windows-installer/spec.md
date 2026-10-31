## Purpose

Provide a verified, reviewable windows installer path for the Engelsiz offline knowledge application.

## ADDED Requirements

### Requirement: Standalone Windows setup
The release SHALL provide Engelsiz-Setup.exe for Windows 10/11 x86_64 containing the app with embedded UI, its adjacent runtime and all required runtime files and notices. Users SHALL NOT need Go, Node, SQLite, Docker or separately installed Kiwix.

#### Scenario: Clean install
- **WHEN** a user installs on a supported clean Windows machine
- **THEN** a shortcut starts Engelsiz, the localhost UI opens and installed content can be read offline

### Requirement: Preserve user data during uninstall
Installation SHALL be per-user without administrator privileges. Uninstall SHALL remove installed program files and shortcuts but SHALL preserve downloaded ZIMs, SQLite state, partial transfers and user configuration in app-data directories. This behavior SHALL be stated in installation instructions.

#### Scenario: Uninstall and reinstall
- **WHEN** a user uninstalls and reinstalls after downloading content
- **THEN** existing user data remains intact and can be reconciled by the app

### Requirement: Verified lawful runtime delivery
Packaging SHALL verify immutable runtime SHA-256 pins and require a reviewed source/license inventory covering the exact binary and incorporated dependencies plus the project's approved license. Missing hashes, notices, source payloads or license decisions SHALL fail closed before a distributable artifact is produced.

#### Scenario: Missing corresponding source
- **WHEN** a runtime archive is present but its complete reviewed corresponding-source bundle is missing
- **THEN** packaging fails with a precise actionable error and does not produce a release artifact
