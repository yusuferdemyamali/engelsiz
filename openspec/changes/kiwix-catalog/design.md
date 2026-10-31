## Context

See proposal.md for motivation and scope. The current provider interface has only lifecycle methods; Kiwix wraps the existing runtime; `api.NewMux()` currently registers health; React is a minimal shell with existing Tailwind tokens. No catalog operation exists and no catalog request belongs on the startup path.

Live evidence, checked 2026-10-29 using small public responses:

- `https://library.kiwix.org/catalog/v2/entries?lang=tur&q=wikipedia&count=10` returns Atom XML, not JSON or the legacy `library.xml` book format. It reported 29 entries, including topical subsets. `count=1&start=1` returned `startIndex=1` and the second entry, confirming offset pagination; these responses had no `rel=next`.
- Full editions use `language=tur`, `name=wikipedia_tr_all`, `category=wikipedia` and `flavour=mini|nopic|maxi`. Entry `id` is a `urn:uuid:…`; `title`, `summary`, `updated`, Dublin Core `issued` and acquisition `link length` are available. Default Atom namespace and `dc:issued` must be handled correctly; pagination elements are in the default namespace despite an unused `opensearch` declaration.
- Open-access links advertise `application/x-zim` yet point to `.zim.meta4`. One tiny referenced Metalink was inspected: it contains mirror URLs, size and MD5/SHA-1/SHA-256 values. Its exact size differed by 90 bytes from catalog `length`; therefore catalog size is reported metadata, not a verified transfer size. OPDS itself provides no checksum in these entries. Dates/URLs observed here are evidence only, never production constants.

## Goals / Non-Goals

**Goals:** Add only a context-aware catalog operation to the existing provider boundary, preserve runtime behavior, and normalize the real OPDS representation into a small stable local API.

**Non-Goals:** No startup networking, persistent cache, Metalink fetching/resolution in production, downloading, checksum validation or broad HTTP/provider framework. No UI redesign; extend the shell using existing tokens and accessible semantic controls.

## Decisions

### Provider and wiring

Add `Catalog(context.Context) ([]CatalogPackage, error)` plus its small normalized value types in `internal/providers`; implement it on the existing Kiwix provider and update existing provider test doubles. Keep the four lifecycle methods unchanged. Give the Kiwix provider a reusable bounded HTTP client and test-injectable endpoint/client using a small options extension (replace the runtime-options alias only as necessary). Inject the same provider into the catalog handler from `app`; do not create a second provider/runtime or make HTTP health depend on catalog availability. A separate capability/plugin hierarchy is unnecessary for one real provider.

### OPDS fetch, selection and bounds

Use the fixed HTTPS catalog endpoint above with `lang=tur&q=wikipedia&count=100&start=0`; server filtering reduces payload but never replaces exact local checks: `language=tur`, `name=wikipedia_tr_all`, `category=wikipedia`, recognized `flavour`. Keep catalog-format handling inside the Kiwix package. Reject entries missing identity/title or a valid HTTPS open-access acquisition link; optional malformed size/date becomes unknown. Treat content strings as plain text.

Use the observed `totalResults`, `startIndex`, `itemsPerPage` to traverse sequential pages with the same fixed filters. Advance by actual entry count; require the returned start to match the requested offset and require progress until total is covered. Bound to 10 pages/1,000 entries, 2 MiB per page, 20 MiB total and a 15-second whole-operation deadline. Fail on invalid/negative/inconsistent counters, empty intermediate pages, repeated pages, bound exhaustion or malformed/truncated XML. Decode through a limit-plus-one reader so overflow cannot appear as a valid truncated success. The HTTP client has a finite timeout, request context cancellation and bounded redirects; close every body. These conservative limits suit the verified 29-entry filtered feed and are easy to exercise with local servers. No arbitrary URL supplied by the frontend is fetched.

Select newest per variant using parsed `dc:issued`, falling back to entry `updated` for ordering only; an unavailable publication date remains null. Known dates sort ahead of unknown; ties use upstream ID then acquisition URL lexically for deterministic selection. Return Mini/Nopic/Maxi order and no fabricated unavailable package entries. Do not infer a release date from a hardcoded filename pattern. An incomplete feed is an error, not a partial catalog.

### Normalized JSON contract

`GET /api/catalog` returns `200 {"packages":[…]}` (empty array, never null). Each package has `provider: "kiwix"`, `providerPackageId` (full upstream ID), `title`, `language: "tr"`, `variant: "mini"|"nopic"|"maxi"`, optional `description`, nullable/omitted `publishedAt`, `updatedAt` and `sizeBytes`, plus `download: {kind: "metalink"|"zim", url, mediaType}`. `publishedAt` maps only to `dc:issued`; `updatedAt` maps to Atom `updated`; the UI uses publication metadata as its release label and can label an update timestamp distinctly if it is the only available date. Positive acquisition `length` supplies `sizeBytes` as reported catalog metadata, not a verified byte total. Detect `.meta4` by URL path regardless of advertised media type; a valid direct `.zim` acquisition can use kind `zim`. Unsupported acquisition formats are skipped. Relative acquisition links, if supplied, resolve against the trusted response URL and must still be HTTPS. Checksums and a resolved ZIM URL are omitted because this operation does not resolve Metalink. This avoids handing a metadata URL to a future downloader as a ZIM.

Keep existing health behavior. Non-GET receives 405 with `Allow: GET`. Failures return 503 JSON `{"error":{"code":"catalog_unavailable","message":"Katalog şu anda yenilenemiyor. Lütfen tekrar deneyin."}}`; technical diagnostics use the existing logger, never raw upstream content in the response. Context cancellation must reach the provider; no independent background fetch survives a request. No CORS dependency is introduced.

### UI

Fetch `/api/catalog` when the view opens; cancel on unmount and avoid overlapping retry attempts. Render the existing app heading plus a responsive “Bilgi Paketleri / Türkçe Wikipedia” section in Mini/Nopic/Maxi order. Use normalized API fields only. Show actual package title/description, localized byte formatting and date/version labels; missing size/date reads “Bilinmiyor”. Missing variants read “Şu anda katalogda yok”, not a fake package. Show a loading status, clear all-empty message, Turkish failure text and a “Tekrar dene” button. Preserve the shell during errors, use status/alert semantics and keyboard-visible controls, and expose no external image/content requests or active download button. No new navigation framework or UI dependency is needed.

## Risks / Trade-offs

- [Catalog evolves or exceeds bounds] → Faithful live-format fixtures, explicit limit errors and a targeted live smoke; no silent partial feed.
- [Catalog availability is network-dependent] → Requests happen only on demand; failure/retry is local to this section. No persistent-cache behavior is promised.
- [Metalink metadata contains more precise transfer information] → Preserve its type and defer resolution to the later download change; no checksum/verification claims now.
- [No installed content flow exists yet] → Verify shell/health and runtime-state isolation now; do not implement the later install/search flow merely to demonstrate offline reading.

## Migration Plan

No schema or user-data migration. Build/embed the updated frontend and run the single validation set in tasks.md. Rollback restores the prior shell/provider API without touching stored data or runtime configuration.
