## 1. Implementation

- [x] 1.1 Implement Linux/Windows GitHub Actions tests and opt-in fail-closed packaging with checksums/artifact upload; validate workflow and release-driver tests.
- [x] 1.2 Create AC-01..18 and DoD 1..16 validation matrix with concrete manual steps and pending/evidence fields; verify complete traceability.

## 2. Distribution acceptance

- [ ] 2.1 Run platform package jobs with approved complete source/license bundles and inspect installer/tarball and checksum artifacts.
- [ ] 2.2 Complete clean-machine/manual acceptance matrix and independently review evidence before declaring v0.1 release-ready.

## Windows candidate evidence (2026-11-02)

A real Windows installer was built locally with the existing fail-closed
`release.py`, using the separately audited MinGW runtime and complete source/notice
payload. See [candidate validation](../../../docs/windows-installer-validation.md).
Task 2.1 remains open because the two native platform **package jobs** with
reviewed committed compliance bundles have not both been executed. Task 2.2
remains open because clean native Windows/manual acceptance and independent
release review have not been completed. The existing Linux alpha is unchanged.
