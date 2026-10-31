## Purpose

Make verified installed ZIM packages readable offline through the managed Kiwix runtime at the Engelsiz origin, with recoverable content failures and private process details.

## ADDED Requirements

### Requirement: Installed registry is the runtime source

The system SHALL activate only privately owned installed ZIMs accepted by installation reconciliation, without requiring the network or upstream metadata. Failed, partial, unregistered and ambiguously owned files SHALL NOT become runtime inputs. Status and content requests SHALL NOT repeat whole-file checksum verification.

#### Scenario: Offline startup with installed content

- **WHEN** Engelsiz starts offline with a reconciled installed package
- **THEN** its ZIM is loaded into the sole managed runtime and becomes available through the Engelsiz origin without a catalog request being required.

#### Scenario: Empty or invalid registry

- **WHEN** no accepted installed ZIM exists
- **THEN** the shell remains usable, runtime remains stopped without requiring its executable, and no unregistered or partial file is served.

### Requirement: Durable installation triggers controlled activation

After an installation is durably committed the system SHALL asynchronously refresh the complete installed runtime set, without requiring an Engelsiz restart. Repeated unchanged sets SHALL reuse a ready child. Runtime activation failure SHALL NOT change an installed package into installation failure or undo its durable commit.

#### Scenario: First successful install

- **WHEN** a verified package is durably installed in a running contentless app
- **THEN** one runtime starts and the package becomes openable after readiness, without reloading or restarting Engelsiz.

#### Scenario: Concurrent installation and shutdown

- **WHEN** installations, runtime retries and application stop overlap
- **THEN** changes are serialized or coalesced to the latest complete committed set, replacement never overlaps the old child, and a winning stop prevents queued activation from spawning a late child.

#### Scenario: Activation fails after installation

- **WHEN** Kiwix cannot start after a package commit
- **THEN** the package remains installed, content reports an actionable runtime failure, and explicit retry can recover it.

### Requirement: Same-origin Kiwix renderer

The system SHALL serve the Kiwix renderer, its local assets, article paths and redirects beneath `/content/` on the Engelsiz origin. It SHALL use Kiwix's supported URL-root facility without rewriting article HTML or implementing a renderer. Existing `/api/*` and frontend assets SHALL retain their routes.

#### Scenario: Open installed content and local resources

- **WHEN** a user opens a ready installed package's supplied href
- **THEN** the main article, local stylesheet, scripts and archive image resolve on the Engelsiz origin under `/content/`, including after a Kiwix main-page redirect.

#### Scenario: Missing content

- **WHEN** a nonexistent article beneath `/content/` is requested
- **THEN** the request receives the content response rather than the Engelsiz SPA fallback.

### Requirement: Proxy is confined to the owned runtime

The proxy SHALL target only the currently ready owned IPv4 loopback child, with positive listener ownership evidence and no caller-selectable destination. It SHALL reject unsafe target/path forms and unapproved redirects, ignore caller Host/forwarding headers as routing inputs, propagate cancellation and close upstream response resources. Public responses SHALL NOT reveal the runtime port, filesystem paths or raw process diagnostics.

#### Scenario: Caller attempts target injection

- **WHEN** a request supplies an absolute URL, network-path target, encoded traversal, foreign Host, forwarding headers or a destination query parameter
- **THEN** those values cannot change the fixed owned upstream or escape the `/content/` route, and no unrelated service is contacted.

#### Scenario: Runtime exits or loses ownership

- **WHEN** runtime readiness or listener ownership is absent, or the upstream connection fails
- **THEN** content fails safely with a friendly response and shell return link, without adopting another listener or exposing internal addresses.

#### Scenario: Redirect confinement

- **WHEN** Kiwix responds with a redirect
- **THEN** only a same-origin path confined beneath `/content/` is emitted; an absolute owned-upstream redirect is converted to that path, and other origins or unsafe paths produce a sanitized failure.

### Requirement: Public runtime status and retry contract

`GET /api/runtime` SHALL return HTTP 200 with `{runtime:{state,error},contents:[{packageId,href}]}`. State SHALL be `stopped`, `starting`, `ready`, `stopping` or `failed`; error SHALL be null except for a sanitized `{code:"runtime_unavailable",message}` in failed state. Contents SHALL contain only ready loaded installed packages, with href `/content/content/<packageId>/`, and SHALL otherwise be empty. `POST /api/runtime/retry` with `{}` SHALL accept an asynchronous retry with HTTP 202 and the same response shape; no installed content SHALL yield 409 `no_installed_content`, and malformed input SHALL yield 400 `invalid_request`. Duplicate pending retry SHALL be idempotent. Responses SHALL be uncached and SHALL NOT contain PID, endpoint, port or local paths.

#### Scenario: Read state and retry failure

- **WHEN** the runtime fails with installed content present and the client submits a retry
- **THEN** the client receives sanitized status, a bounded asynchronous attempt is accepted, and later status reports either ready content hrefs or another friendly failure.

### Requirement: Shell survives content failure and offers minimal controls

The system SHALL keep the shell, health endpoint, catalog/download controls and installed metadata usable after runtime startup failure or unexpected exit. The installed-packages view SHALL show an accessible friendly runtime status, explicit retry on failure and open links only for ready content; it SHALL refresh state while visible and abort polling when hidden or unmounted. No custom search or article UI SHALL be introduced in this change.

#### Scenario: Missing executable with installed package

- **WHEN** Kiwix cannot start because its executable is missing
- **THEN** Engelsiz still serves a healthy shell, installed metadata remains visible, content failure and retry are available, and technical diagnostics are restricted to logs.

#### Scenario: Visible runtime lifecycle

- **WHEN** a successful install activates content or a ready child later exits
- **THEN** visible installed-package controls reflect readiness or failure without a page reload, with no internal runtime port shown.
