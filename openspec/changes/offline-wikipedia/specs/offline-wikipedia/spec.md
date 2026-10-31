## Purpose

Enable users to search verified installed Wikipedia content and read its articles locally while retaining Engelsiz navigation, including when the online catalog is unavailable.

## ADDED Requirements

### Requirement: Installed-package search API

The system SHALL expose `GET /api/search?q=<query>&packageId=<installed-id>` using the selected locally loaded package and Kiwix search. A successful response SHALL contain `query`, `packageId`, `totalResults` and a `results` array of at most 25 objects with string fields `title`, `snippet`, `source`, `articleId`, and `href`. Text fields SHALL be plain text; missing optional upstream snippet/source SHALL become empty strings. Each returned href SHALL be a safe same-origin `/content/content/<selected-id>/...` article path. The API SHALL neither contact the online catalog nor build a separate content index.

#### Scenario: Positive local search
- **WHEN** a nonempty query matches articles in a selected installed and loaded package
- **THEN** the response is HTTP 200 with matching article titles, available snippets/source and safe article identifiers/links, with totalResults reflecting Kiwix's count

#### Scenario: Empty and unmatched queries
- **WHEN** a valid selected package is searched with a trimmed empty query or a query with no matches
- **THEN** the response is HTTP 200 with `results: []` and `totalResults: 0`, and an empty query performs no upstream search

### Requirement: Bounded requests and explicit search failures

Search SHALL accept exactly one `q` and one `packageId`, rejecting unknown or duplicated parameters, malformed encoding, control characters, queries longer than 256 Unicode code points and invalid package identifiers with HTTP 400 `invalid_request`. Packages absent from the verified installed snapshot SHALL return HTTP 409 `content_unavailable`; a selected installed package with an unavailable owned runtime SHALL return HTTP 503 `runtime_unavailable`; a ready runtime that has not loaded that package SHALL return HTTP 409 `content_unavailable`; upstream timeout SHALL return HTTP 504 `search_timeout`; malformed, oversized or failed upstream responses SHALL return HTTP 502 `search_unavailable`. Errors SHALL use `{"error":{"code":"...","message":"..."}}` with a user-readable Turkish message and no private ports, paths or upstream bodies. Unsupported methods SHALL return HTTP 405 with `Allow: GET`.

#### Scenario: Invalid target input
- **WHEN** a request supplies an arbitrary upstream URL, duplicate parameters or an invalid package identifier
- **THEN** it fails with HTTP 400 and does not contact an arbitrary listener

#### Scenario: Failed runtime or search
- **WHEN** the selected package cannot be served, the runtime is unavailable, or search fails/times out
- **THEN** the API returns the corresponding explicit error and the UI offers recovery without presenting the failure as no results

### Requirement: Search and reading interaction

The UI SHALL offer search independently of catalog success, list installed packages, and distinguish initial empty input, loading, no installed package, no matches and recoverable errors. Search SHALL use only the Engelsiz API. Superseded responses SHALL not replace current results. Titles and snippets SHALL render as text. Selecting a result SHALL display Kiwix-rendered content under an Engelsiz shell with a visible return to search and normal navigation; returning SHALL preserve the query, selected package and results. A truncated result set SHALL clearly indicate that only the first 25 matches are shown.

#### Scenario: Article reading and return
- **WHEN** a user searches, selects a result, follows an internal article link and returns to search
- **THEN** articles render through the same-origin content proxy, the shell's return control remains available, and the earlier search state is preserved

#### Scenario: Accessible asynchronous states
- **WHEN** search starts, finishes empty or fails
- **THEN** a labeled keyboard-operable form and status/error announcements communicate the state without removing application navigation

### Requirement: Offline availability and evidence

After a package is installed, cold application startup, installed-package listing, positive search and article reading SHALL work without external network access. Catalog failure SHALL remain independent of those operations. Required application/article styles, scripts, fonts and images SHALL be available locally; optional external hyperlinks SHALL not be required for reading.

#### Scenario: Actual isolated offline restart
- **WHEN** a real indexed ZIM is installed in a disposable profile and the app is restarted with its owned Kiwix process inside an isolated network namespace with only loopback enabled and no external routes, with the fixture source stopped
- **THEN** installed-package listing, positive `/api/search`, a result article and its required local resources succeed while catalog access fails, and evidence records isolation, requests and child cleanup

#### Scenario: Browser with unavailable catalog
- **WHEN** the browser uses the actual app with its catalog/download fixture source stopped
- **THEN** installed packages, search, article reading and return navigation remain usable on one Engelsiz origin, with no private Kiwix port exposed

### Requirement: Preserve the owned serving boundary

Search and article requests SHALL reuse the owned generation-bound transport and existing `/content/*` route security. Stale generations and unproven/reused listeners SHALL fail closed. No endpoint SHALL accept a client-supplied upstream target or expose the private runtime address. The feature SHALL retain production Turkish Wikipedia catalog filtering and SHALL not introduce a custom Wikipedia parser/renderer, full-text index, SQLite FTS or AI dependency.

#### Scenario: Generation replacement during search
- **WHEN** the owned runtime exits or changes while a search transport is pending
- **THEN** that request cannot adopt the replacement or a foreign listener and returns a recoverable failure
