# v0.1 failure-mode validation

Scope: docs/changes/09 and main-spec sections 20–22. Automated tests use small local fixtures; this document does not claim a clean Windows installation or release acceptance run.

| Failure or boundary | Expected behavior | Implementation and validation |
| --- | --- | --- |
| Internet/catalog unavailable | Keep installed content, local state and retry visible | api/catalog_test.go failure isolation; api/updates_test.go offline/local state; real Kiwix offline restart in zim-update/verification.md |
| Disk space unavailable/full | Fail preflight or transfer safely; preserve partial bytes | download/manager_test.go TestSpacePreflightAndRestartPreservesPartial, TestRemainingCapacityAndInjectedDiskFullRetry, TestUnavailableCapacityFailsClosed |
| Interrupted transfer | Persist partial and allow explicit resume; no startup transfer | download/manager_test.go TestReopenReconcilesAndResumesWithoutAutomaticTransfer, TestPauseConcurrentControlsCancelAndShutdown |
| Checksum/size mismatch | Never select/install corrupt content; old release usable | packages/service_test.go TestVerificationFailuresAndExplicitRetry; packages/updates_test.go verification failure; real invalid-hash smoke |
| Runtime cannot start or candidate cannot open | Shell stays usable, explicit error/retry; update restores committed set | app/runtime_test.go startup failure; app/update_activation_test.go probe/commit/rollback failure; real invalid-ZIM smoke |
| Port collision | Select alternate owned loopback port | app/app_test.go fallback tests; runtime/kiwix/runtime_test.go collision/ownership tests |
| SQLite corruption | Fail explicitly, log failure, preserve original bytes | database/db_test.go TestCorruptDBNoReset; app/app_test.go TestCorruptDatabaseStartupLogsWithoutBrowser |
| Migration failure | Transaction rollback, no destructive reset | database/db_test.go TestFailedMigrationRollsBack and metadata preservation tests |
| Metadata path traversal/platform-special name | Reject before filesystem mutation | providers/kiwix/acquisition_test.go; download/manager_test.go unsafe paths; packages/service_test.go source/directory/root replacement cases |
| Content proxy path traversal | Reject encoded/ambiguous escape paths | api/content_test.go TestContentConfinement and literal-percent regression |
| Runtime command injection | Direct process arguments, no shell | runtime/kiwix/runtime_test.go relative override/paths with spaces and metacharacters; runtime uses exec.Command |
| Private paths/ports or raw errors in browser | Stable public error DTOs; omit internal fields | api/packages_test.go public field allowlist/storage failures, api/runtime_test.go, api/downloads_test.go |
| Unbounded logs | Rotate at 1 MiB; current plus two backups; bound individual records | app/logging_test.go rotation/reopen/oversized/concurrent/permission/hostile-path/error tests |
| Sensitive upstream/runtime log output | No remote bodies, credentials, queries or article output | download/diagnostics_test.go TestTransferDiagnosticsOmitRemoteErrorsAndMetadata and preflight/storage assertions; api/catalog_test.go TestCatalogDiagnosticsOmitSensitiveOversizedUpstreamErrors; runtime/kiwix/runtime_test.go TestRuntimeOutputDiagnosticsOmitQueriesBodiesAndCredentials; packages/updates_test.go verification diagnostic assertion; provider catalog/acquisition privacy test |

Manual release validation still required on both target systems: trigger network loss and runtime failure in a packaged application, confirm readable UI and retry, check writable data-directory diagnostics and ensure installer uninstall preserves content. Packaging changes define their platform-specific clean-environment checklists.
