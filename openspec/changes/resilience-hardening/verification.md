# Verification: resilience-hardening

Date: 2026-10-30. Scope: docs/changes/09, proposal/design and all three requirements in specs/resilience-hardening/spec.md. Independent review covered the rotating writer, producer diagnostics, expected failure behavior, filesystem/process boundaries and docs/failure-mode-matrix.md.

## Findings and disposition

No unresolved critical or warning finding remains in the reviewed code.

- **Resolved: sensitive producer error strings.** Catalog and content proxy handlers now log stable operation/code/error type rather than raw errors. Kiwix catalog/acquisition methods also log safely, so failures reached indirectly by update/download actions remain observable. Generic download diagnostics include only a validated generated ID and allowlisted code, omitting metadata, URLs and custom transport/write error text.
- **Resolved: child output privacy.** `internal/runtime/kiwix/runtime.go` records stdout/stderr byte counts with `content=omitted`; failure diagnostic suffixes contain captured byte lengths. Raw stream tails remain bounded private memory and are never inserted into logs, public status or errors. `TestRuntimeOutputDiagnosticsOmitQueriesBodiesAndCredentials` verifies both streaming and startup summaries using repeated sensitive input.
- **Resolved: diagnostic test synchronization.** The asynchronous transfer privacy test closes/joins the manager before reading its buffer, avoiding a test-only buffer race.
- **Rotating writer:** the review found no correctness blocker. Fixed filenames are confined to a pinned root; directory/file permissions are restrictive; symlink/nonregular slots and changed file identities fail closed. Current log plus two backups are capped at 1 MiB each; individual records are bounded. Writer failures are latched and reported on Close. Synchronization covers writes, rotation and closure within one process.

## Requirement coverage

| Requirement | Evidence |
| --- | --- |
| Bounded private operational logs | `internal/app/logging.go`, logging tests for rotation/reopen/permissions/oversized records/concurrency/unsafe slots/replacement/close failures. App lifecycle and database failures retain local technical diagnostics. Runtime operational metadata and stream byte summaries remain available without user content. Download privacy/preflight/storage tests, catalog sensitive oversized-error regression, provider privacy regression and package verification diagnostic assertion cover producer boundaries. |
| Recoverable failures preserve local data | Matrix maps catalog/offline isolation, disk preflight and simulated exhaustion, partial-transfer recovery, verification mismatch, runtime startup/probe failure, port collision, database corruption and transactional migration rollback to concrete automated tests. Real Kiwix success/failure/offline restart evidence is in `../zim-update/verification.md`. No database reset or old-content deletion was introduced. |
| Filesystem/process boundaries remain constrained | Existing owned-root installation/download tests cover traversal, hostile names, symlink targets, inode/directory/root replacement and rename collisions. Content tests reject encoded traversal and scrub private origins/headers. Runtime launch uses direct process APIs; the argv regression includes spaces and shell metacharacters. Public package/runtime/download responses use stable DTOs and omit private filesystem/port fields. |

## Validation and limits

- Reviewer ran `go test -race ./internal/app -run 'TestRotatingLog|TestLoggerFailure'`: passed (`internal/app 1.029s`). Earlier app/provider suites and coordinator race checks also passed during update implementation.
- Producer regression files and final code were re-read after the diagnostic sanitization changes. Final aggregate Go/race/frontend results belong to the orchestrator's final validation; the prior suite run must not be represented as testing later edits.
- `docs/failure-mode-matrix.md` covers every requested operational failure and the explicit path/process/public-error/logging boundaries. It distinguishes deterministic fixture evidence from pending packaged clean-machine acceptance.
- Synchronization is per process, as stated in the design; this change does not provide multi-instance shared-directory coordination.
- Windows/Linux clean-environment package installation, offline operation and uninstall/reinstall checks remain packaging/release acceptance work. They are not inferred from unit tests or cross-compilation.

Assessment: reviewed resilience behavior and diagnostics match the planning contract, subject to final aggregate checks. No task checkboxes were changed by this review.

## Final orchestrator checks

After the final producer sanitization changes, `go test -race ./...` passed for all packages and `go vet ./...` passed. Linux and Windows amd64 applications compiled with embedded production frontend assets. Frontend search/update tests and TypeScript/Vite production build passed. `git diff --check` passed. These checks complete this change's implementation verification; platform distribution acceptance remains tracked separately.
