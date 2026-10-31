## Purpose

Provide a verified, reviewable release pipeline path for the Engelsiz offline knowledge application.

## ADDED Requirements

### Requirement: Platform release build matrix
CI SHALL build and test linux-amd64 and windows-amd64, install locked frontend dependencies, build/embed frontend assets, run Go tests, stage verified runtimes, package platform artifacts and produce SHA-256 checksums. Build artifacts SHALL be available for review without publishing a release automatically.

#### Scenario: Successful candidate build
- **WHEN** all test, source/license and packaging gates pass
- **THEN** reviewable platform packages and their SHA-256 files are uploaded as CI artifacts

### Requirement: Traceable acceptance evidence
Release documentation SHALL map every AC-01 through AC-18 and each of the sixteen Definition of Done items to automated checks or concrete manual steps. Unexecuted Windows/Linux clean-machine criteria SHALL remain pending and block a v0.1 readiness claim.

#### Scenario: Unexecuted clean Windows test
- **WHEN** a cross-compiled installer exists but has not been installed on a clean supported Windows machine
- **THEN** AC-01 remains pending rather than being inferred from compilation

### Requirement: Verified lawful runtime delivery
Packaging SHALL verify immutable runtime SHA-256 pins and require a reviewed source/license inventory covering the exact binary and incorporated dependencies plus the project's approved license. Missing hashes, notices, source payloads or license decisions SHALL fail closed before a distributable artifact is produced.

#### Scenario: Missing corresponding source
- **WHEN** a runtime archive is present but its complete reviewed corresponding-source bundle is missing
- **THEN** packaging fails with a precise actionable error and does not produce a release artifact
