## Context

See proposal.md. The application already has typed public errors, owned filesystem roots, direct process launch and recovery tests. The remaining work should reuse these boundaries. Logging currently appends indefinitely and download.Options has no logger wired from the app.

## Goals / Non-Goals

**Goals:** Bound diagnostic storage, record operation failures safely, audit failure behavior with precise evidence.

**Non-Goals:** A new telemetry service, general refactoring, automatic database repair, or changes to transfer/provider behavior beyond concrete failure gaps.

## Decisions

- Implement a small synchronized rotating writer in the app logging boundary, with fixed maximum file size and a small fixed backup count. Enforce restrictive permissions, reject symlink/nonregular destinations and close cleanly. Avoid a new logging dependency.
- Log bounded operation identifiers and error codes. Preserve technical context that is safe, while sanitizing URLs/credentials and omitting remote bodies/search terms. Wire the existing download error observer/logger capability if available; otherwise add a narrow optional logging hook.
- Keep public error mapping typed and stable. Audit existing API/runtime handlers before editing; add only missing mappings and diagnostic points.
- Deliver a failure matrix mapping each requested failure to code/tests, including manual tests where deterministic local simulation is insufficient. Validate filesystem/process boundaries through existing tests and focused new regressions rather than replacing working implementations.

## Risks / Trade-offs

- Rotation renames can fail → return an explicit log error without deleting content or resetting data; keep logging bounds and close errors observable.
- Multiple process instances can share a data directory → guarantee synchronization within one process; do not invent a multi-instance coordinator in this change.
- Diagnostic redaction can lose detail → retain operation name, stable error code and bounded safe internal detail.

## Migration Plan

No database migration. Existing log files are rotated on initialization if oversized; preserve a bounded previous history. Existing content and download records remain authoritative.
