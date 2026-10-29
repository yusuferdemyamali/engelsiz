## Purpose

Discover full Turkish Wikipedia editions from Kiwix's live catalog and present trustworthy package metadata through Engelsiz's local API without compromising offline application use.

## ADDED Requirements

### Requirement: Live full Turkish Wikipedia discovery

The system SHALL discover full Turkish Wikipedia editions from live Kiwix catalog metadata, restrict results to Turkish full Wikipedia Mini, Nopic and Maxi, and return at most one latest available release per variant in Mini/Nopic/Maxi order. It SHALL NOT embed dated ZIM URLs, release dates or package sizes as production constants. Relevant catalog pages SHALL be considered within bounded resource limits; an incomplete or invalid traversal SHALL fail rather than silently claim a complete catalog.

#### Scenario: Broad feed contains unrelated editions

- **WHEN** the feed contains full Turkish Wikipedia editions alongside topical Wikipedia subsets, other languages, other collections and unknown flavours
- **THEN** only full Turkish Wikipedia Mini/Nopic/Maxi editions are returned, with the newest metadata-selected release for each variant.

#### Scenario: Variant appears on another page

- **WHEN** qualifying entries span catalog pages
- **THEN** the normalized result includes qualifying variants from those pages or reports a bounded catalog failure if traversal cannot complete safely.

### Requirement: Honest normalized package metadata

Each available package SHALL expose provider identity, upstream package ID, title, language, normalized variant, available description, publication/version metadata, size in bytes and typed acquisition metadata. Optional missing or invalid metadata SHALL remain absent/null. A Metalink acquisition SHALL be distinguished from a direct ZIM URL; checksums and resolved ZIM URLs absent from the catalog SHALL NOT be fabricated or implied to be resolved.

#### Scenario: Real Kiwix Metalink acquisition

- **WHEN** an OPDS acquisition advertises `application/x-zim` but links to `.zim.meta4`
- **THEN** its original URL is returned as a Metalink acquisition, its catalog length is represented as catalog-reported bytes when valid, and no direct ZIM URL or checksum is invented.

#### Scenario: Optional fields absent

- **WHEN** a qualifying entry has no usable publication date, size or description
- **THEN** the entry retains its available identity and acquisition metadata while unknown fields remain absent/null and the UI labels unknown date/size clearly.

### Requirement: Same-origin catalog API

The system SHALL expose `GET /api/catalog` as JSON containing a `packages` array of normalized metadata. The frontend SHALL access catalog data only through this local Go API and SHALL NOT call Kiwix catalog, acquisition, illustration or runtime endpoints directly. Other HTTP methods SHALL receive 405; catalog failures SHALL receive a stable sanitized JSON error rather than upstream internals.

#### Scenario: Successful and empty responses

- **WHEN** catalog discovery completes with available packages or no qualifying packages
- **THEN** the API returns HTTP 200 and a `packages` array containing those packages or an empty array respectively.

#### Scenario: Provider request fails

- **WHEN** upstream networking, status, parsing or traversal fails
- **THEN** the API returns HTTP 503 with code `catalog_unavailable` and a user-facing message, without upstream response bodies, runtime addresses or local paths.

### Requirement: Bounded failure isolation

Catalog operations SHALL honor request cancellation, have finite network and response-processing limits, close response resources and reject malformed, truncated or oversized feeds. Catalog work SHALL NOT be required during application startup or mutate local content/runtime state. Failure SHALL leave the shell, health endpoint and existing provider lifecycle usable.

#### Scenario: No network or stalled upstream

- **WHEN** a catalog request cannot complete because the network is unavailable or upstream stalls
- **THEN** the request fails within its deadline and Engelsiz remains usable without restarting or stopping the content runtime.

#### Scenario: Hostile or broken feed

- **WHEN** a response exceeds configured bounds, contains malformed XML or cannot advance pagination
- **THEN** catalog discovery returns a controlled failure without exposing a partial success or crashing the application.

### Requirement: Package view and retry

The “Bilgi Paketleri” view SHALL support Mini, Nopic and Maxi with upstream title/description, human-readable size and publication/version when available. It SHALL show loading, unavailable variants, an empty result and a recoverable catalog failure distinctly. Error copy SHALL explain that the catalog cannot currently be refreshed without asserting that internet connectivity is definitively absent. A retry SHALL fetch the catalog again. Download/install actions SHALL NOT be offered as working features in this change.

#### Scenario: Partial or empty catalog

- **WHEN** discovery returns fewer than all three variants
- **THEN** available variants show actual metadata, missing variants are labelled unavailable and an entirely empty result has a clear empty message without fabricated package records.

#### Scenario: Failure followed by recovery

- **WHEN** catalog loading fails and the user retries after upstream recovers
- **THEN** an understandable Turkish error and retry control are replaced by the successful package display while the application remains running.
