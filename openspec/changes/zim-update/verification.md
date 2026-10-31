# Verification: zim-update

Date: 2026-10-30. Scope: docs/changes/08 and specs/zim-update/spec.md.

## Behavior and evidence

| Contract | Evidence |
| --- | --- |
| Strict newer compatible release; no automatic transfer | packages/updates_test.go comparison, compatibility and concurrent reservation tests; frontend update tests and keyboard browser smoke |
| Trusted explicit action; duplicates/stale input rejected | api/updates_test.go strict body, upstream identity, stale release, duplicate and catalog-unavailable cases |
| Active selection independent of installed files | packages/updates_test.go commit/offline restart, create/attach crash gap, interrupted activation and persisted installing-candidate recovery |
| Candidate-specific usability before commit | providers/kiwix/usability_test.go book 404, missing membership, safe/unsafe redirects; app/update_activation_test.go successful replacement |
| Full rollback and unrelated variants preserved | app/update_activation_test.go probe/commit/rollback failures and package selection tests |
| No uncommitted public readiness; shutdown cannot restart | app/update_activation_test.go snapshot/search/content gates, captured transport and cancellation cases |
| Recoverable errors, retained versions, offline local state | api/updates_test.go local-only read; frontend/tests/updates.test.mjs and browser checks |

## Real runtime integration

Used existing Kiwix Tools 3.8.2 and a 259145-byte real indexed ZIM fixture, with a local catalog/metalink/transfer server and a fresh isolated XDG profile. No production Wikipedia archive was downloaded.

Passed scenarios:

1. Initial verified install, Kiwix search and article request.
2. Explicit newer-release download, verification, candidate usability and commit; old file retained.
3. Cold restart after success selects the committed new release.
4. Wrong SHA-256 fails installation while the old selected release stays searchable/readable.
5. A non-ZIM payload with a correct transfer hash passes file verification but fails runtime usability; the previous runtime selection is restored and remains searchable/readable.
6. Stop the catalog fixture, cold restart after the failed update, verify committed old selection and offline search/article access; update comparison reports unavailable and preserves local operation errors.

Local evidence: /tmp/engelsiz-update-verification/evidence.json and smoke.py; isolated run /tmp/engelsiz-update-smoke-yfawbsfw. Browser script: /tmp/engelsiz-update-ui-smoke.cjs; desktop/mobile screenshots: /tmp/engelsiz-update-desktop.png and /tmp/engelsiz-update-mobile.png. Browser verified identity-only POST, keyboard update, progress, catalog failure/retry, active-only search, 390px layout and no page errors. These temporary files supplement the committed regression tests.

## Independent review

Registry/API and coordinator/usability implementations were cross-reviewed by separate agents. Review found and resolved variable-width timestamp recovery ordering, candidate version association, newer release supersession after failure, and cancelled/creating UI state handling. Crash recovery at the persisted install intent boundary has an explicit regression test. No known critical implementation finding remains; final aggregate test results are recorded below when complete.

## Aggregate checks

- `go test -race ./...`: all packages passed, including API, app, registry, provider and real child-process fixture tests.
- `node --test tests/*.test.mjs`: search/update frontend suites passed.
- `npm run build` and `bash scripts/build-frontend.sh`: TypeScript and production frontend build passed; assets copied for Go embedding.
- Windows amd64 application cross-compilation passed. This proves compilation, not execution on Windows.
- Frontend ESLint and desktop/mobile browser checks passed in the UI implementation review.
- `git diff --check`: passed.

The real-runtime integration was repeated after the resilience diagnostics changes in a second fresh profile (/tmp/engelsiz-update-smoke-c6t5s3uv). All five update/restart/failure scenarios passed again; the latest evidence.json contains that run.
