## Why

The main offline workflow now has explicit recovery paths, but operational logs are unbounded and failure visibility and security boundaries need systematic verification before packaging. Predictable failures must preserve local content and give users a useful recovery action.

## What Changes

- Add bounded file logging with simple rotation and restrictive permissions, covering lifecycle, ports, provider, transfer, verification and storage failures.
- Keep technical failure details in bounded logs and return stable Turkish user-facing errors without private paths or stack traces.
- Audit existing offline/catalog, disk, interruption, checksum, runtime, port and database failure handling; fill concrete gaps without broad refactoring.
- Verify filesystem containment, process argument safety and sensitive/body logging boundaries using a documented failure matrix and targeted regression tests.

## Capabilities

### New Capabilities

- `resilience-hardening`: Recoverable local failures, bounded operational logging and security-boundary verification for the v0.1 workflow.

### Modified Capabilities

None. Existing change contracts remain the baseline.

## Impact

Go app log initialization, download diagnostics, API error mapping and regression tests, plus a source-backed failure-mode matrix. No new external service or runtime dependency. Installer, release distribution and automatic repair/destructive database reset remain outside this change.
