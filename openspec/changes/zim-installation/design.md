## Context

See proposal.md for motivation and specs/zim-installation/spec.md for the behavioral contract. Change04's download descriptor stores neither checksum nor language/version/content type. Its completed `.part` is deliberately unverified; `Manager.recover` currently fails a completed record when that file disappears. Runtime discovery/refresh is a later change. Files and SQLite cannot share a transaction, and ZIM hashing can take minutes.

## Goals / Non-Goals

**Goals:** preserve exact acquisition trust, establish one owner for finalization, and recover either side of atomic rename without compromising other transfer controls.

**Non-Goals:** content parsing, SQLite article import, serving refresh, replacement/update, cross-volume copy fallback, or a general provider installation framework.

## Decisions

1. **Capture at acquisition, verify offline.** Extend provider-resolved metadata and generic download persistence with content type, language, version and checksum algorithm/value. Kiwix parses SHA-256 only from the exact selected Metalink file, validates a 64-digit hex digest, and rejects ambiguous/conflicting supported hashes. Retain safe existing download behavior when no supported hash exists, but reject installation with 422. Copy release version and display data from the selected catalog entry during creation; do not re-resolve it at install. Generic downloads store opaque values without Wikipedia logic. Re-fetching at install was rejected because it can validate a different release or require internet.

2. **Small package service and additive schema.** Add `internal/packages` with repository, safe public snapshot and installer; wire its API in `internal/api` and lifecycle in `internal/app`. Add matching embedded/reference migration copies for immutable download metadata, generic artifact handoff state, and `content_packages`. One unique `download_id` gives stable retry identity; provider/package identity plus destination reservation prevents a second download silently overwriting the same release. Registry contains all required private source/local-path metadata and public fields specified in the delta. Legacy columns remain nullable; no migration invents a checksum/version. A fresh download is the remedy for missing legacy metadata. A separate service avoids coupling multi-gigabyte hashing into HTTP handlers or provider catalog logic.

3. **Brief transfer claim, long cancellable hash outside manager locks.** The manager exposes a narrow internal claim/handoff of an owned completed record. Under its operation lock it checks completed state and durable artifact ownership and reserves the artifact for one package; it then releases that lock. The installer serializes duplicate work per artifact and performs streaming SHA-256 with context cancellation outside `Manager.ops`. Repeated active calls return the existing 202 snapshot; installed calls return 200; failed calls explicitly retry the same row. Keep download state completed: artifact ownership/consumed state is separate and generic, so manager recovery knows a missing consumed `.part` is intentional. Holding the operation mutex through hashing was rejected because it blocks pause/cancel on unrelated transfers.

4. **Intent before atomic rename.** Use application-controlled ID-based `.zim` basenames under `content/kiwix`; never a catalog-provided path. Check directory/source ownership, regular-file status, symlinks, containment and existing destinations using rooted filesystem operations. Hash and size-check the actual owned source, close handles as required on Windows, persist `installing` with intended source/destination and verification metadata, then rename within the same user-data filesystem. No copy fallback or overwrite of an unrelated destination is allowed. Finally commit installed status, installed timestamp and generic consumed ownership together. Source remains on hash/rename failure; final remains after a post-rename DB failure for reconciliation. Public errors use stable codes/readable Turkish messages; details stay in logs.

5. **Explicit recovery ordering and failure table.** After directories and migrations, initialize package recovery before ordinary download reconciliation; the latter must understand durable claimed/consumed ownership even if a claimed source is absent. Recovery runs before accepting API work, and rehashes candidates rather than trusting `installing` alone:

   | Persisted state and files | Recovery |
   |---|---|
   | verifying/installing; source only | Re-verify saved bytes, then resume finalization or persist failed |
   | installing; intended final only | Verify final size/hash and ownership, then atomically mark installed plus consumed |
   | installed; intended final valid | Preserve installed and completed download history |
   | missing files, both paths, invalid/foreign final | Explicit failed/unavailable state; preserve evidence, never overwrite or blindly delete |

   Installed records whose final file is missing or invalid cannot remain trusted installed listings. Storage failure during reconciliation aborts safe initialization rather than auto-resetting data. App-owned workers use lifecycle context, not request context; stop/join them before closing download roots and SQLite. Reconciliation of local accepted work may run on startup without initiating downloads or network access.

6. **Stable UI/API boundary.** Use the exact Package, list and install response/status contract in the delta spec. Validate metadata, eligibility and known collision synchronously; byte verification failures become persisted asynchronous failed packages. A collision discovered after acceptance likewise becomes failed with a collision code. Extend the existing downloads handler route for install and add a package listing handler; do not expose paths/URLs. Frontend uses package snapshots for installed variant/version and stage/error display, keeping download completion copy truthful and polling bounded by view visibility.

## Risks / Trade-offs

- [Filesystem/SQLite split] → Durable intent plus hash-based restart reconciliation; rename failure retains the source and post-rename DB failure retains the final.
- [Large hashes] → Streaming bounded memory, cancellable lifecycle, no transfer-wide locks; restart may repeat verification.
- [Different volume or permission/storage failures] → Clear failure and retained evidence; no non-atomic copy fallback.
- [Legacy acquisitions cannot prove identity] → Preserve history and require fresh acquisition; never borrow a current release checksum.
- [Unexpected local files] → Stable ID paths, unique reservations and fail closed on ambiguity; no user-managed import workflow.

## Migration Plan

Apply additive SQLite migration to both migration locations; test upgrade from change04 with real legacy rows. Keep existing transfers usable with null verification metadata. Start package reconciliation before download recovery, then initialize workers/API. No destructive down migration or automatic database reset: rollback uses the earlier binary only after stopping workers and preserving database/content backup; consumed download rows require the new recovery logic. Validate with deterministic HTTP/Metalink fixtures, failure injection at intent/rename/commit boundaries, restart/reopen checks, a real small ZIM fixture and frontend production checks. No new runtime dependency is needed; use Go SHA-256 and existing SQLite/React facilities.
