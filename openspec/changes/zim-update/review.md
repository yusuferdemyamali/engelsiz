# Review: zim-update

Date: 2026-10-30. Reviewed against docs/changes/08, proposal.md, design.md and all six requirements/scenarios in specs/zim-update/spec.md. This pass independently reviewed registry/recovery, API and UI code; coordinator/usability code was also cross-reviewed by the registry agent. No task checkboxes were changed by this review.

## Findings

No unresolved critical or warning finding identified. The following findings from implementation review have concrete fixes and regressions:

- **Crash association ordering:** variable-width RFC3339Nano strings cannot be compared lexically. `internal/packages/updates.go:66` now parses timestamps before matching reservation/download chronology. `TestUpdateReservationSurvivesCreateAttachGap` covers `.1Z` versus `.11Z` and a restart before association attachment.
- **Candidate identity:** `internal/packages/repository.go:110` associates installation with both upstream identity and candidate version; installed status, accepted file identity and update readiness publish under the service lock.
- **Interrupted install recovery:** `internal/packages/service.go:80` and `:97` run update recovery before and after file reconciliation. A recovered installing candidate becomes interrupted before the coordinator can run. `TestPersistedInstallingCandidateRestartDoesNotAutoActivate` covers this boundary.
- **Failed release supersession:** a failed/interrupted candidate does not permanently prevent a later upstream release. `internal/packages/updates.go:231` preserves history while releasing the old operation reservation; API comparison exposes the later offer. Covered by `TestFailedCandidateCanBeSupersededByLaterUpstreamRelease` and `TestLaterOfferVisibleAfterCandidateActivationFailed`.
- **Public state:** cancelled and creating states have explicit UI labels, retained candidates cannot appear as active search choices, and shutdown exposes stopping with no candidate links. Covered by frontend update tests and coordinator tests.

## Contract coverage

| Requirement | Implementation and evidence |
| --- | --- |
| Compatible strictly newer release, no automatic transfer | `NewerRelease`/`CompareUpdates` parse publication dates and require provider/content/language/variant equality plus a different upstream identity. They only construct offers. `TestCompareUpdatesStrictCompatibleDates` and API comparison tests cover uncertain/equal/older/mismatched releases. |
| Explicit trusted update and existing transfers | `StartUpdate` resolves trusted acquisition metadata, validates exact size/SHA-256, rechecks active file ownership and compatibility, then persists a unique reservation before generic manager creation. Strict API bodies accept only the selected identity. Existing download/install controls handle pause/resume/cancel/retry. `TestUpdateValidationAndConcurrentReservation` and `TestUpdateHTTPRejectsUntrustedAndStaleInput` cover duplicate/stale/untrusted actions. |
| Candidate usability and durable commit | `activation.activateOne` substitutes only the old package in the complete previous snapshot. `CheckContent` requests that book using the captured owned transport and constrains redirects. Commit atomically swaps active selection with operation success; old files remain installed. Usability tests cover missing book/404/unsafe redirects; coordinator and selection tests cover success and unrelated variants. |
| Failure preservation/rollback | Transfer/verification do not change active selection. Candidate/probe/commit failures restore the entire previous runtime input; restoration failure preserves the durable old selection and reports runtime failure. Cancellation prevents rollback launches after shutdown. Coordinator failure/gating tests and `TestUpdateVerificationFailurePreservesOldAndAllowsNewAttempt` exercise these branches. |
| Restart safety | Active selection is separate from installation, seeded once by migration. Installed snapshots filter through selection and accepted file identity. Recovery converts precommit readiness to interrupted without automatic retry; committed success survives offline restart. Selection, interruption, association-gap and install-intent recovery tests cover the boundaries. |
| Accessible presentation and offline local recovery | `frontend/src/updates.tsx` shows installed/candidate versions, unknown-size label, transfer/install/activation states, typed errors and explicit buttons with live status/alerts. Local polling preserves operation state while avoiding repeated catalog access. `packages.tsx` distinguishes retained files; `search.tsx` offers active packages only. API local mode retains operations on catalog failure. Frontend tests and the browser evidence in verification.md cover keyboard action, mobile layout and failure presentation. |

## Validation and limits

- Reviewer ran the app/provider suites and targeted race tests covering coordinator, candidate usability, captured public transport and shutdown; these passed. The final strengthened targeted race run passed for app in 1.041s and provider from cache. `git diff --check` passed.
- Root's real Kiwix integration and browser checks are recorded in verification.md: verified update, retained file, cold restart, checksum failure, unusable-but-correctly-hashed archive, rollback, offline search/reading and unavailable comparison.
- Generic transfer/path ownership tests remain relevant inherited coverage; no production Wikipedia archive was downloaded.
- Runtime replacement uses one child and briefly interrupts content. Staging is reported as loading. Existing in-flight searches/response headers can finish before staging acquires its gate; their bounded transport timeouts limit this wait.
- Clean Windows/Linux installer acceptance is outside this change and must remain tracked by the packaging/release changes. This report does not claim those environments were executed.

Assessment: the reviewed implementation covers the specified update behavior. Archive readiness still depends on the final aggregate checks and task completion recorded by the orchestrator.
