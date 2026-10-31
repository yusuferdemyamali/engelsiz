## 1. Durable update and selection model

- [x] 1.1 Add additive active selection and update-operation persistence, seed existing installations once, exclude candidates across install/restart, and verify with registry/recovery tests.
- [x] 1.2 Implement strict compatible newer-release comparison and trusted explicit update creation with duplicate/stale protection and generic transfers; verify date, metadata, concurrency and restart scenarios.

## 2. Safe activation

- [x] 2.1 Serialize candidate activation with refresh/retry, gate public readiness/content/search, prove candidate-specific usability and commit selection only after success; verify successful update and unrelated-package preservation.
- [x] 2.2 Restore prior selection/runtime on failure, persist recoverable interruption/error state, prevent shutdown restarts, and verify candidate failure, commit failure and offline restart boundaries.

## 3. API and interface

- [x] 3.1 Expose update comparison/local state, strict update creation and local activation retry through same-origin APIs; verify malformed, stale, duplicate and catalog-unavailable requests.
- [x] 3.2 Show explicit update action, installed/new versions and size, linked transfer/install progress, activation errors/recovery and retained versions; verify keyboard/status accessibility and existing search/download flows.

## 4. Integration and review

- [x] 4.1 Run Go tests/race checks, frontend tests/lint/build and platform compilation; record evidence and exercise tiny-fixture success, failed update/rollback and offline restart.
- [x] 4.2 Independently review the implementation against all update scenarios, resolve findings and deliver a verification report with remaining limitations.
