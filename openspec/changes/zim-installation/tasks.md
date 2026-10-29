## 1. Acquisition and persistence

- [x] 1.1 Extend selected-file Kiwix acquisition and generic download descriptors with immutable content type/language/version/exact size/SHA-256; verify valid, missing, unsupported, malformed/conflicting hashes and catalog changes with provider fixtures, without fetching metadata at install time.
- [x] 1.2 Add matching embedded/reference additive migrations and repositories for verification metadata, generic artifact claim/consumed state and the complete content_packages registry; verify fresh database creation, legacy change04 upgrade, nullable legacy metadata and unique retry identity with database tests.

## 2. Installation and recovery

- [x] 2.1 Implement brief completed-artifact claim and cancellable app-owned package worker, streaming size/hash verification and rooted atomic finalization; verify success, mismatch, duplicate/retry identity, ownership/symlink/collision refusal and that hashing does not block unrelated download controls.
- [x] 2.2 Persist installing intent before rename and implement reconciliation before normal download recovery plus shutdown worker joins; verify reopen at verification/intent/rename/commit boundaries, rename/DB failures, missing/ambiguous files and completed download history after consumed .part removal.

## 3. API and interface

- [x] 3.1 Wire GET /api/packages and POST /api/downloads/{id}/install to the exact delta contract; verify 200/202 idempotency, 400/404/409/422/500 failures, installed filtering, request-independent work, offline listing and omission of source URLs/internal paths with handler tests.
- [x] 3.2 Extend existing frontend download/package views with install, stages, readable Turkish failure/retry and installed variant/version; verify keyboard/status semantics, hidden/unmounted polling cleanup and frontend lint/type/production build checks.

## 4. Integration acceptance

- [x] 4.1 Exercise the real app with a small actual ZIM via deterministic HTTP/Metalink acquisition: matching SHA installs once, wrong SHA never installs, missing metadata is rejected and offline process reopen preserves final bytes, registry and completed transfer; record API/browser observations without adding parsing/serving/update scope.
- [x] 4.2 Run relevant Go tests (including race-sensitive install/control coverage), frontend required checks and openspec validate zim-installation --strict; verify every delta scenario is covered by a test or recorded integration observation and document any platform-specific limitation.
