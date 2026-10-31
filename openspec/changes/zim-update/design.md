## Context

See proposal.md for motivation and specs/zim-update/spec.md for the contract. Packages currently verify, rename and register files; every installed file is then passed to the single owned Kiwix process. Search membership is bound to a runtime generation. Update candidates must not enter that serving set merely by completing installation.

## Goals / Non-Goals

**Goals:** Distinguish durable installation from committed serving selection, coordinate activation and rollback with ordinary refresh/retry, and preserve offline recovery across every commit boundary.

**Non-Goals:** Parallel runtime children, automatic candidate retry at startup, destructive old-file cleanup, or changes to the generic transfer engine's provider independence.

## Decisions

### Durable selection and update associations

Add SQLite active-package selection separate from content_packages status, plus update operation records relating the previous package, candidate upstream identity and download. Existing verified installations are seeded into selection once; later update candidates are excluded until commit. Ordinary first installations retain their current automatic activation behavior. Installation remains managed by the existing package service, including exact size, SHA-256, filesystem ownership and recovery. Selection-aware snapshots drive runtime and search. Lists distinguish active and retained files.

Alternatives rejected: selecting all installed files silently publishes candidates after restart; choosing the latest version on startup bypasses usability verification. Runtime inputs alone are not a durable commit.

### Trusted comparison and explicit transfer

Compare strict publication dates and distinct upstream IDs within provider/content/language/variant. GET /api/updates returns local operation details even if comparison fails, and indicates unavailable comparison separately. POST /api/packages/:id/update accepts only providerPackageId, resolves trusted metadata again and validates compatibility/newness against the still-active package. Use download.Manager for the transfer and persist its association before installation can publish it. Reject duplicate/conflicting updates. Existing download controls and explicit install stay available; POST /api/updates/:id/activate retries a verified candidate after a failed/interrupted activation without requiring the catalog.

### Serialized activation and rollback

The app activation coordinator serializes refresh, retry and update activation. It retains the complete prior committed selection, substitutes only the matching old package with the verified candidate, starts that set using ReplaceZIMPaths and proves the candidate book with an actual content request through the owned generation-bound transport. Root readiness is insufficient. Only then commit selection and operation success together. On any failure restore the previous complete set, preserve both files, persist a readable error, and keep the prior selection committed even if runtime restoration fails.

Expose a loading gate to runtime status, search and content while a candidate is being probed or restored, so uncommitted generation readiness is never public success. Cancellation checks precede replacement and commit; shutdown cancels and joins the coordinator before stopping provider/package resources. No rollback child starts after shutdown.

### Restart and UI

Startup reconciles files, restores only committed selection, and marks interrupted activation recoverable without automatically reattempting it. The UI shows installed/candidate versions, metadata size, explicit update action, linked download/install progress, activation/recovery errors, and retained versions. Reuse established components and transfer controls; keep requests on the app origin and use live regions for state changes.

## Risks / Trade-offs

- Single-child refresh interrupts content briefly → report loading and restore previous selection on failure.
- Installation commit can precede activation → durable association and selection filtering prevent accidental startup publication.
- SQLite commit fails after candidate starts → restore old runtime and leave old selection authoritative.
- Catalog fails → retain local operation/error details and offline reading; never interpret failure as no update.
- Retaining versions consumes disk → existing preflight checks account for new transfer; old-file deletion stays explicit future work.

## Migration Plan

Apply an additive migration and seed existing installations without deleting files. Recovery rechecks verified registry files before serving them. Validate with small deterministic fixtures and the existing tiny real ZIM fixture, never download a production Wikipedia archive. Rollback of an update is an app-coordinated selection restoration; schema downgrade is not automatic.
