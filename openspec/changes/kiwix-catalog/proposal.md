## Why

Engelsiz has an offline-capable shell and managed Kiwix runtime but cannot discover available Turkish Wikipedia packages. A live catalog integration lets users compare real Mini, Nopic and Maxi metadata without embedding release-specific URLs or sizes.

## What Changes

- Read Kiwix's live OPDS catalog through the existing Go provider boundary and normalize only the full Turkish Wikipedia Mini/Nopic/Maxi editions.
- Expose `GET /api/catalog` and a Turkish “Bilgi Paketleri” view with current metadata, loading, missing-variant, empty, retry and catalog-unavailable states.
- Preserve upstream acquisition metadata honestly: current catalog links are Metalink (`.zim.meta4`) resources even though their media type is `application/x-zim`; they are not direct ZIM URLs.
- Keep catalog failure isolated from startup, health, existing runtime lifecycle and local application use.

## Capabilities

### New Capabilities

- `kiwix-catalog`: Live Turkish Wikipedia discovery, normalized metadata, local catalog API and package-selection display.

### Modified Capabilities

None. Existing lifecycle requirements remain intact; the sole provider gains only the catalog operation needed here. There are no main specs under `openspec/specs/` yet.

## Impact

Touches `internal/providers`, `internal/providers/kiwix`, API/app wiring and the existing React shell. Uses Go's standard HTTP/XML support and current frontend tooling; no generic client framework or new provider architecture is required. Source: `docs/changes/03-kiwix-katalog-ve-türkçe-wikipedia-varyantları.md` and `docs/main-spec.md` sections 8–10, 18–20, AC-05/06/11/16. The obsolete `[[engelsiz-main_spec]]` reference resolves to `docs/main-spec.md`.

Out of scope: downloads, Metalink resolution, checksum verification, installation/registry, search, article reading, update comparison, packaging and other Kiwix collections. No persistent catalog cache or speculative provider methods.

## Acceptance Criteria and Tests

1. A live Kiwix response yields at most one latest package per recognized variant, ordered Mini, Nopic, Maxi. Backend tests use real-format Atom XML containing Turkish full editions, topical subsets, other languages and duplicate releases; pagination must not hide a variant.
2. Package ID, title, language, variant, description, publication/version metadata, byte size and acquisition metadata come from upstream. Missing optional values remain null/omitted and display “Bilinmiyor”; no fabricated release dates, sizes, direct URLs or checksum values. Fixtures include the real `.zim.meta4` link semantics.
3. The browser calls only Engelsiz's same-origin API. API tests cover normalized success/empty responses, unsupported methods and a stable sanitized failure envelope; provider errors never become raw frontend stack traces.
4. Bounded HTTP requests and XML processing fail cleanly on timeout/cancellation, bad status, malformed/truncated/oversized feeds and invalid pagination. Deterministic tests use local HTTP servers, not a live service.
5. The package view supports loading, available variants, unavailable variants, empty results, failure and retry. A catalog outage leaves health, shell and provider lifecycle usable; verify recovery on retry without an app restart.
6. Run one full validation set: frontend build/embed script, Go tests with race detector, Go vet, Linux and Windows compile checks, OpenSpec strict validation, and one targeted browser smoke for normal/error/retry states. No actual ZIM transfer is performed.
