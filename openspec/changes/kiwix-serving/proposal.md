## Why

Verified installed packages are durable after change 05, but the runtime still receives no registry inputs and content has no same-origin route. This change makes those packages readable offline through the existing Kiwix renderer while keeping the Engelsiz shell available when Kiwix fails.

## What Changes

- Feed reconciled, privately owned installed ZIM paths to the sole runtime at startup and refresh its complete input set after a successful installation commit.
- Serialize set changes and explicit retry with existing start/stop ownership guarantees; never overlap child processes or undo a winning application stop.
- Serve Kiwix beneath `/content/` through a Go reverse proxy to the currently ready, positively owned loopback child; keep ports, paths and diagnostics private.
- Add a precise runtime status/retry API and minimal installed-package open links and friendly runtime states. Preserve catalog, downloads and installed records on runtime failure.
- Acceptance covers offline reopen, install-triggered refresh, resources/redirects, failed runtime recovery, proxy confinement and lifecycle races. Search UI, custom rendering/FTS and updates remain outside this change.

## Capabilities

### New Capabilities

- `kiwix-serving`: Installed-content activation, safe same-origin proxy, shell survival and minimal runtime/open-content contract.

### Modified Capabilities

None. Earlier change artifacts remain historical; the new serving capability extends the completed runtime and installation foundations without rewriting them.

## Impact

Backend ownership: `internal/packages`, provider/runtime lifecycle, `internal/app`, `internal/api` and focused tests. UI ownership: `frontend/src/packages.tsx`, a runtime client/view module and, only if needed, `App.tsx`; embedded frontend output belongs to final integration. No database migration or new production dependency is expected. Real-binary compatibility evidence uses official Kiwix 3.8.2 with the existing tiny test ZIM, never a bundled production fixture.
