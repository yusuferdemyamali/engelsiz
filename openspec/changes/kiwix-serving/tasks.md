## 1. Runtime and registry foundations

- [x] 1.1 Runtime owner: add complete installed-set replacement and loaded-generation snapshots within existing lifecycle serialization, including unchanged-set reuse, empty sets and Stop epoch invalidation; verify delayed/concurrent refresh/retry/Stop tests preserve one-child ownership and existing runtime tests pass.
- [x] 1.2 Runtime owner: configure `--urlRootLocation=/content`, readiness at `/content/`, and private generation-bound proxy dispatch with post-dial ownership verification and no mutex held during streaming; verify child-exit/port-reuse rejection, listener proof failure and existing competing-readiness regressions.
- [x] 1.3 Backend owner: expose a trusted private installed snapshot after reconciliation and add a lock-free post-commit notification into a coalescing app refresh coordinator; verify only committed owned installed files are selected, no status/proxy read rehashes ZIMs, concurrent commits retain the full set, and runtime failure does not change installation success.

## 2. Same-origin integration and UI

- [x] 2.1 Backend owner: mount confined GET/HEAD `/content/` proxy plus `/content` redirect, sanitize errors/Location/forwarding headers, and implement the exact GET `/api/runtime` and async POST `/api/runtime/retry` contract in design.md; verify safe article paths/query/cancellation, hostile URL/Host/path/redirect inputs, methods/body validation and absence of ports/paths/diagnostics in public responses.
- [x] 2.2 Backend owner: connect reconciled startup inputs and successful-install refresh, keep shell/browser/health alive on ordinary Kiwix failures, and cancel/join refresh before package/database closure; verify empty profile, missing executable, unexpected child exit, explicit retry and shutdown integration tests.
- [x] 2.3 UI owner: implement visible-only runtime polling/retry and installed-package content links against the fixed API, preserving installed status and existing controls; verify build plus accessible loading/empty/starting/ready/failed states, pending activation, abort-on-hide/unmount and no client construction of runtime ports.

## 3. Integration verification

- [x] 3.1 Root integration: regenerate embedded frontend; run relevant Go tests/race checks, full Go/frontend build and Windows cross-build, then exercise real official Kiwix 3.8.2 with a fresh isolated profile and one tinyZIM through actual provider download→install→refresh→same-origin main-page/local-assets, offline reopen, child exit/retry and missing-binary shell survival; record evidence and confirm owned-child cleanup and `openspec validate kiwix-serving --strict` PASS.
