## 1. Backend foundations

- [x] 1.1 Add the downloads migration/repository and generic transfer/snapshot types from design.md; verify migration preserves existing data and persists/reloads nullable totals, errors and state using a temporary SQLite database.
- [x] 1.2 Add provider-side trusted package acquisition resolution, including bounded Metalink-to-file metadata handoff and direct-file support; verify local XML fixtures resolve actual file URL/size and reject malformed/unsafe metadata without treating `.meta4` as payload.

## 2. Transfer engine and recovery

- [x] 2.1 Implement streaming `.part` writes, progress, safe validated Range continuation and full-response restart; verify httptest cases for exact payload, short body, ignored Range, mismatched ranges, changed entity, absent validator and416 without concatenated corruption.
- [x] 2.2 Implement Linux/Windows capacity preflight plus recoverable write errors; verify insufficient-space rejection before writes, remaining/full-size checks on resume/restart and injected ENOSPC preserving actual partial bytes for successful retry.
- [x] 2.3 Implement queued worker ownership, persisted pause/resume/cancel/retry and startup/shutdown recovery; verify controls under concurrency, manager/database reopen with valid partial continuation, file/DB reconciliation and shutdown joining workers before database close.

## 3. Application and UI integration

- [x] 3.1 Wire provider resolution, manager lifecycle and download HTTP handlers using the exact design.md contract; verify API creation/list/detail/control/error/status behavior, duplicate requests and rejection of arbitrary URLs/paths without regressing catalog/runtime tests.
- [x] 3.2 In frontend files only, add catalog download actions and Downloads view with typed snapshots, Turkish status/errors, byte progress, eligible controls and visibility-aware polling; verify TypeScript/production build and keyboard-accessible tiny-transfer UI flow. This task can run alongside backend work once design.md's API contract is shared; backend owns Go/migrations/app wiring.

## 4. Integrated acceptance

- [x] 4.1 Run one full Go test/race set, Windows compilation and frontend build after integration, then a controlled local tiny-file browser smoke covering start/pause/resume/retry/cancel and persisted reopen state; record commands/results and confirm completed files remain unverified `.part` with no install/runtime refresh. Repeat focused checks only for subsequent fixes.
