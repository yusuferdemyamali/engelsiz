## 1. Provider and API

- [x] 1.1 Implement the KiwixProvider search contract and bounded RSS XML normalization using the captured owned ContentTransport; verify positive, zero-result, mixed highlight text, missing optional metadata and production-style renamed-fixture responses against the official 3.8.2 probe.
- [x] 1.2 Add GET /api/search with verified installed-snapshot validation, exact parameter/status/error contract and application wiring; verify successful/blank/invalid/unknown-package/unavailable-runtime/timeout responses and that catalog is never called.
- [x] 1.3 Add meaningful adversarial and lifecycle regression coverage for unsafe article links, malformed/oversized RSS, redirect refusal, canceled requests and stale/unproven/foreign runtime listeners; verify focused Go tests and race tests preserve the change-06 transport guarantees.

## 2. Search and reading UI

- [x] 2.1 Add the Turkish search view and navigation using the frozen design.md API types, installed-package selector and accessible initial/loading/no-package/no-results/error/result states; verify frontend typecheck/build, out-of-order request protection and catalog-error independence.
- [x] 2.2 Add a persistent Engelsiz reading shell around a titled iframe using the existing same-origin Kiwix renderer, with return to preserved query/package/results and keyboard focus; verify by CUA that result clicks, internal links and return/navigation work without exposing the private runtime port.

## 3. Offline acceptance and integration

- [x] 3.1 Build the embedded app and run a real verified tiny-ZIM install, source/app stop and cold reopen in a disposable loopback-only network namespace; record interfaces/routes, failed catalog, installed-package listing, positive search, article/required-local-resource responses and owned-child cleanup under /tmp/engelsiz-orchestration-2026-10-29/offline/, explicitly documenting the English fixture boundary.
- [x] 3.2 Run the actual host-browser CUA acceptance with metadata source stopped, including installed packages, search/error/no-results, article/internal navigation/return and same-origin required-resource audit; run go test ./..., relevant race tests, go vet ./..., frontend build and Windows cross-build, then strict OpenSpec validation and scope review with evidence recorded.
