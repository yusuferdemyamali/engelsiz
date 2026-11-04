## Context

The application already owns a provider-independent resumable HTTP Download Manager, a loopback-only Go API, a React shell and native Windows/Linux distribution. Translation is a separate manual-text capability, not an article-proxy transform or a replacement content provider. The OpenSpec CLI is unavailable in this environment; artifacts are maintained directly in the established repository format. The user explicitly authorizes implementation, not a planning-only stop.

## Decisions

- Use Argos Translate with two pinned model artifacts. Actual upstream bytes, SHA-256 and licenses are researched before finalizing the catalog; never accept model URLs from API clients.
- Reuse `download.Manager.Create` for transfer and `ClaimCompleted(downloadID, downloadID)` for installation ownership. Do not introduce an external provider, alternate downloader or automatic startup transfer.
- Expose `GET /api/translation`, `POST /api/translation/models/{id}/download`, `POST /api/translation/models/{id}/install` and `POST /api/translation/translate`. Explicit download/install lifecycle avoids an additional background monitor. Status reads are side-effect free.
- Install each verified ZIP independently under the normal data root `content/translation/argos-tr-en` or `argos-en-tr`. Hash and size verification precede confined extraction; reject traversal, symlinks, case collisions and excessive expansion. Stage then atomically publish; reconcile final installation and consumed download ownership after interruption.
- Bundle a native frozen persistent Python/Argos worker at app-relative `runtime/translation/engelsiz-translate[.exe]`. No user Python, shell, pip or model downloads at runtime. A developer binary override follows existing runtime conventions.
- Communicate over bounded JSONL stdin/stdout. The model root is a fixed argv value; requests contain only the supported pair and text. Reuse loaded CPU models. Argos sentence segmentation must be explicitly offline; disable sockets defensively without claiming a portable kernel sandbox.
- Go caps input at 5,000 Unicode characters with a separate bounded JSON body. A single global context-aware gate bounds inference. Timeouts, malformed output and worker crashes terminate/reap the child; the next request may start cleanly. Discard runtime stderr and never log/store text. Shutdown interrupts inference and joins the child before closing dependent resources.
- Return model direction status independently: `missing`, `installing`, `ready`, `error`, with the ordinary download snapshot attached. An absent reverse model never disables a ready direction.
- Designer exclusively owns frontend work and uses design-taste-frontend. Reuse theme tokens/components, preserve navigation and generic download recovery, prevent stale results after input/direction changes, and expose clear copy/paste/focus/error feedback.
- Native runtime packaging is mandatory for both release targets. Frozen Python's dynamic dependencies require an explicit checked policy, rather than falsely labeling it a static ELF executable. Record exact native versions/licenses and preserve release fail-closed behavior.

## Verification and risks

Small fixture ZIPs/fake workers establish deterministic safety and lifecycle boundaries. Real upstream models and the real frozen Linux engine establish actual translation through the running application. A network namespace with no external interfaces establishes offline operation; host networking is not modified. Full repository checks run only after writer reconciliation. Windows cross-build and packaging tests are useful but cannot establish actual Windows runtime compatibility; clean-machine/native execution limits remain explicit.

Initial architecture risk check accepted the three disjoint writer lanes and required explicit install rather than a background monitor. Remaining empirical risks are model provenance, Argos hidden NLP downloads, native shared-library packaging and cold/warm resource consumption.
