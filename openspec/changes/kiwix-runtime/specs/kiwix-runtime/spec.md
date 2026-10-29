## Purpose

Provide a minimal content-provider lifecycle boundary and a managed, private Kiwix child runtime without requiring content or an installed Kiwix binary for an empty application shell.

## ADDED Requirements

### Requirement: Minimal provider lifecycle boundary

The system SHALL route content-runtime lifecycle operations through a minimal provider boundary with Kiwix as its sole concrete provider. The boundary SHALL cover only ensure-start, stop, explicit restart and runtime status needed by this change, without unimplemented content features or a plugin framework.

#### Scenario: Core manages content runtime

- **WHEN** the application starts or closes its configured content provider
- **THEN** lifecycle coordination uses the provider boundary and Kiwix command details remain inside the concrete provider/runtime integration.

### Requirement: Contentless startup remains usable

The system SHALL leave the runtime stopped when no local content inputs are configured and SHALL keep the existing localhost shell and health/browser startup working without locating or requiring a Kiwix executable.

#### Scenario: Fresh profile without Kiwix

- **WHEN** Engelsiz starts with no configured content and no installed Kiwix binary
- **THEN** no Kiwix child is started, no missing-runtime failure is produced, and the normal shell becomes available.

### Requirement: Deterministic executable resolution

The system SHALL support an explicit development executable path and otherwise resolve the platform-specific Kiwix executable under the application's adjacent runtime directory, independently of the working directory. Resolution or execution failures SHALL be reported when runtime startup is requested and SHALL NOT silently fall back from an invalid explicit override.

#### Scenario: Development override

- **WHEN** startup is requested with an explicit executable path containing spaces
- **THEN** that exact executable is used without shell interpretation.

#### Scenario: Default executable

- **WHEN** runtime startup is requested without an override
- **THEN** resolution uses `runtime/kiwix-serve` on Linux or `runtime/kiwix-serve.exe` on Windows relative to the Engelsiz executable rather than the current directory.

#### Scenario: Missing executable

- **WHEN** the selected executable cannot be resolved or started
- **THEN** the operation returns a contextual error, runtime state becomes failed with no active PID, and technical details are recorded in the application log.

### Requirement: Private foreground child with readiness

The system SHALL start Kiwix as a directly owned foreground child with explicit `127.0.0.1` binding, a private available port and separate trusted local content-path arguments. The system SHALL report ready only after bounded HTTP 200 and positive OS evidence that the selected IPv4 loopback TCP listener belongs to that exact started child PID, while that child remains alive and startup has not been cancelled. Elapsed time, HTTP success or child liveness alone SHALL NOT establish listener ownership. If ownership cannot be proved within the startup deadline, startup SHALL fail closed and clean up its owned attempt. The internal address, port and filesystem paths SHALL NOT be exposed through public API, browser navigation or UI.

#### Scenario: Successful start

- **WHEN** configured local content is supplied and the child starts serving its welcome page
- **THEN** bounded HTTP 200 and exact-child listener ownership are verified before readiness succeeds within the configured deadline, the runtime becomes ready and its live PID is tracked internally.

#### Scenario: Readiness failure or startup cancellation

- **WHEN** the child exits early, never becomes HTTP-ready, or startup is cancelled
- **THEN** startup returns an error and cleans up any owned child and output resources before it completes; cancellation is a controlled stop and other startup failures are recorded as failed.

#### Scenario: Port handoff collision

- **WHEN** another process takes a selected port before Kiwix binds it, including while a delayed-bind child remains alive and the competing listener returns HTTP 200
- **THEN** the competing response cannot satisfy owned readiness, that process is neither adopted nor killed, the failed owned attempt is reaped, and startup either retries a bounded number of fresh loopback ports or returns a contextual startup failure.

#### Scenario: Listener ownership evidence unavailable

- **WHEN** OS listener ownership evidence is unavailable, unreadable or does not match the exact child PID and selected IPv4 loopback listener
- **THEN** the runtime does not become ready based on HTTP success or a fixed delay; if proof remains absent until the startup deadline, startup returns a contextual error after owned cleanup.

### Requirement: Serialized lifecycle and unexpected-exit tracking

The system SHALL prevent overlapping owned Kiwix processes for one provider instance, track lifecycle state and live PID, and detect and reap unexpected child exits without automatically restarting in a loop. Repeated or concurrent ensure-start requests SHALL share an in-progress attempt or use the already-ready child.

#### Scenario: Concurrent start requests

- **WHEN** multiple callers request runtime startup concurrently
- **THEN** at most one child exists, callers observe the same attempt's outcome, and an already-ready child is reused.

#### Scenario: Unexpected exit

- **WHEN** a starting or ready child exits outside a requested stop, even with exit code zero
- **THEN** it is reaped, its active PID is cleared, failed state and an exit diagnostic are available internally, and no automatic restart occurs.

#### Scenario: Stop races with start

- **WHEN** stop or application cancellation occurs while readiness is pending
- **THEN** the pending start is cancelled and cannot leave a late-started owned child after stop completes.

### Requirement: Explicit restart preserves single ownership

The system SHALL provide an explicit controlled restart which stops and reaps the current owned child before starting its replacement, preserving the configured executable and content inputs and returning the new startup outcome.

#### Scenario: Restart a ready runtime

- **WHEN** restart is explicitly requested
- **THEN** the old child has terminated and been reaped before the replacement is spawned, and readiness is checked again.

#### Scenario: Restart after unexpected failure

- **WHEN** restart is requested after a previous child failed
- **THEN** prior cleanup is completed and a single new bounded startup attempt can recover the runtime or return a new failure.

#### Scenario: Stop interrupts replacement readiness

- **WHEN** Stop is requested while an explicit restart's replacement is delayed or never becomes ready
- **THEN** stop intent and startup cancellation are published promptly without waiting for the readiness deadline, the replacement is stopped and reaped before Stop completes, restart returns a cancelled outcome, and that restart or an already queued restart cannot report readiness or launch a late child after Stop wins.

### Requirement: Owned-child application shutdown

The system SHALL stop and reap its own Kiwix child on controlled application shutdown, cancellation and startup rollback before closing resources used by the runtime. Stop SHALL be idempotent, have a bounded termination path with forced termination when necessary, and SHALL NOT terminate unrelated processes.

#### Scenario: Controlled application shutdown

- **WHEN** SIGINT, SIGTERM or application cancellation initiates shutdown
- **THEN** the owned runtime is terminated and reaped, its private port is released and no owned child remains after the application returns.

#### Scenario: Child ignores graceful termination

- **WHEN** the owned child does not exit within the graceful-stop deadline
- **THEN** the application force-terminates that child, waits for cleanup and reports any remaining cleanup error.

#### Scenario: Short or cancelled Stop context

- **WHEN** Stop is called with a short deadline or an already-cancelled context while startup or restart readiness is pending
- **THEN** stop intent still cancels the pending attempt promptly and bounded owned termination/reaping proceeds without waiting for the startup deadline; a caller context error does not substitute for cleanup or permit a late child.

#### Scenario: Unrelated Kiwix process exists

- **WHEN** Engelsiz shuts down while an independently launched process is running
- **THEN** only the child referenced by this provider's active process ownership is stopped and the unrelated process remains alive.

### Requirement: Bounded process diagnostics

The system SHALL independently capture stdout and stderr without unbounded memory accumulation or output-induced lifecycle deadlock, and SHALL record process start, readiness, unexpected exit, restart, stop and failure context through the existing application logging facility.

#### Scenario: Both output streams and long output

- **WHEN** the child writes to stdout and stderr, including output larger than a normal line buffer
- **THEN** both streams are drained, stream identity is retained, buffering is bounded and the child can still complete startup or shutdown.

#### Scenario: Startup error diagnostics

- **WHEN** a child fails during startup after writing diagnostics
- **THEN** the operation returns a contextual runtime error and its captured stderr/stdout diagnostics remain available in logs without exposing them to frontend responses.
