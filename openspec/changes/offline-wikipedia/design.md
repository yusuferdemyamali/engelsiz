## Context

See proposal.md for motivation. Change 06 already activates the verified installed snapshot, names installed files `<32 lowercase hex packageId>.zim`, starts one owned Kiwix generation with `--urlRootLocation=/content`, and proxies `/content/*` using `ContentTransport`. Public runtime status contains package IDs and relative content links, never the internal port. React currently separates catalog, downloads and installed packages; no search view exists.

Actual probe evidence (2026-10-30): `/tmp/engelsiz-orchestration-2026-10-29/offline/example-search-probe.json` and `0123456789abcdef0123456789abcdef-search-probe.json`. Official kiwix-tools 3.8.2 with the existing 259145-byte upstream libkiwix `example.zim` returns **RSS XML** for `GET /content/search?content=<zim-basename>&pattern=wiki&format=xml`. Both original and production-style renamed fixtures returned two matches. `format=json` returns HTML and MUST NOT be used. The fixture already has a working full-text index; no additional archive download is needed. RSS item fields are `title`, `link`, `description` (mixed XML nodes including `<b>`), `book/title`, and `wordCount`; channel includes OpenSearch totalResults/startIndex/itemsPerPage. Item links already use `/content/content/<zim-basename>/<escaped-article-path>`.

## Goals / Non-Goals

**Goals:** Freeze a small shared HTTP contract so Go and React work can proceed in disjoint files; use the existing trust boundary and renderer; prove an actual offline restart.

**Non-Goals:** New provider architecture, schema migrations, custom rendering/indexing, catalog dependency during search, whole archive mirroring, pagination UI or linguistic-quality claims from the English test fixture.

## Decisions

### 1. Fixed API and React contract

Request: `GET /api/search?q=<URLSearchParams-encoded-text>&packageId=<32-lowercase-hex-id>`. Both parameters occur exactly once, no extras. Trim Unicode whitespace; reject control characters/invalid UTF-8 and more than 256 query code points. Return trimmed query. Blank trimmed input yields an empty result only after package availability validation; the UI does not need to send blank input.

```ts
type SearchResult = {
  title: string; snippet: string; source: string;
  articleId: string; href: string;
}
type SearchResponse = {
  query: string; packageId: string; totalResults: number;
  results: SearchResult[];
}
type SearchFailure = { error: { code: string; message: string } }
```

Success is always HTTP 200 with an array, including no matches. `articleId` is the decoded article path suffix after `/content/content/<packageId>/`; `href` is the validated relative, escaped upstream article link. No invented IDs or arbitrary URL resolver. Source comes from `item/book/title`, with empty string when absent; snippet comes from description as XML character data in document order (including highlighted child text), normalized whitespace, at most 1000 code points. Never pass XML/HTML through `dangerouslySetInnerHTML`. Missing titles use articleId; missing description/source remain empty strings. Return at most 25 rows, with nonnegative integer totalResults from the RSS channel. UI shows a first-25 notice if count exceeds rows.

Failures: 400 `invalid_request`; 405 `method_not_allowed` (`Allow: GET`); 409 `content_unavailable` for unknown/not-loaded package; 503 `runtime_unavailable` if a loaded generation cannot be acquired; 504 `search_timeout`; 502 `search_unavailable` for HTTP/format/size/parse failures. Body is SearchFailure with a Turkish message. Responses set JSON content type and `Cache-Control: no-store`. No raw error, local filename or upstream URL in JSON. Check selected ID against the package service verified installed snapshot first (409 if absent), then check runtime readiness (503 if not ready), then membership of the loaded generation (409 if absent). Supply the local installed-snapshot check at API/app wiring; the provider owns readiness, loaded-path validation and actual search. No catalog call is permitted.

This is a single selected-package, first-page API, keeping ordering/ranking entirely Kiwix-owned. Alternatives of multi-package aggregation and new pagination are unnecessary for this slice.

### 2. Provider-owned bounded RSS adapter

Use a small optional `Search(context.Context, packageID, query)` provider interface and shared value/error types, without expanding lifecycle-only mocks unnecessarily. App registers the API once. KiwixProvider acquires one generation's `ContentTransport` and its fixed target; validates selected package against loaded/installed local state; derives only the trusted basename; requests `/content/search` with `content`, `pattern`, `format=xml`, `start=0`, `pageLength=25` via `url.Values`. Use a fresh request and a client bound solely to that transport, proxy environment disabled, redirects disabled, 10-second request context and 1 MiB response cap. Do not reuse the external catalog HTTP client. Empty query does not call Kiwix.

Decode RSS 2.0 using `encoding/xml`, not Wikipedia HTML parsing. Require the expected RSS/channel/count structure; reject malformed XML, invalid count, oversized body and unsafe links. Extract plain XML text without interpreting markup. Bound returned title/source and rows as well. Validate each link as relative (no scheme/authority/userinfo), with no query/fragment, backslash, controls, ambiguous escapes, repeated slash or decoded dot traversal, and an exact selected-package prefix. Apply the same repeated-decoding safety as the content proxy. Preserve valid encoded article paths and Unicode. Reject unsafe upstream payloads as search_unavailable instead of making them clickable. The content proxy still independently validates every clicked href.

Reusing generation transport preserves before-write listener ownership proof and stale-generation rejection. Plain http.Get, a frontend Kiwix search URL, or a newly created upstream URL from request metadata would bypass this guarantee and are excluded.

### 3. Search view and existing renderer

Add `frontend/src/search.tsx` containing the shared response types and view, with navigation wiring in App.tsx. Populate choices from `/api/packages?status=installed`; use existing runtime state/retry UI where useful. One installed package auto-selects; several get a labeled select. Search is usable even if catalog state is error. A labeled form submits intentionally, with AbortController/request identity guarding rapid queries/package changes and unmount. Initial empty guidance, no installed package with a link to packages, loading, no matches, error/retry and result count are distinct states. Turkish copy follows existing UI and focus/status conventions.

Keep search state mounted in its view while reading. A result opens a titled iframe whose src is the validated same-origin href; an Engelsiz header outside the iframe contains a visible “Aramaya dön” control and navigation. Kiwix handles article markup, styles, scripts and internal article navigation unchanged. Return reveals the preserved results and focuses the search/results heading. No HTML injection, content rewrite or new renderer. Existing `/content/*` failure pages remain usable inside the frame. Root acceptance verifies layout and keyboard behavior using CUA only.

### 4. Ownership for implementation

Backend owns `internal/providers/**`, `internal/api/**`, `internal/app/**`, and meaningful Go regression tests. UI owns `frontend/src/search.tsx`, `frontend/src/App.tsx`, and narrowly necessary frontend styles. The frozen contract above is the synchronization point; neither side changes it silently. Root owns acceptance evidence, artifact task updates, final frontend embed/build, integration and review. No new production binary/fixture is committed.

## Risks / Trade-offs

- RSS format varies by bundled Kiwix version → pinned official 3.8.2 live probe plus recorded RSS fixtures; malformed format fails explicitly.
- XML highlighting can lose words with a flat chardata decoder → accumulate character data recursively and test mixed description nodes.
- Article path decoding can escape a package → exact package prefix plus existing proxy-equivalent traversal checks and adversarial tests.
- Runtime changes during search → captured generation transport; no retry onto a different generation within the request.
- English Wikibooks fixture does not prove Turkish search quality or large archive performance → state that boundary; production remains Turkish-only; no large download required here.
- Stopping a metadata source is not full network isolation → actual namespace test plus a separate host browser test.

## Migration Plan

No data migration. Build frontend and embedded app together; rollback removes the search API/view without changing installed packages or existing serving.

Acceptance evidence goes under `/tmp/engelsiz-orchestration-2026-10-29/offline/`. Reuse the real 3.8.2 binary and tiny indexed ZIM with the existing isolated nopic profile/harness, without shipping harness code. Root runs the real app and Kiwix inside `unshare --user --map-root-user --net` (verified possible with loopback enabled) or the already proven `bwrap --unshare-net` disposable bundle approach. Record interfaces/routes proving only loopback and no external route. Within the namespace, install via the normal local OPDS/Meta4/download verification flow, stop source and app, restart the same installed profile, confirm catalog failure, installed package API, positive search (`wiki`), same-origin article and required embedded resources, then graceful child cleanup. Build/prepare all binaries before isolation; no global networking changes. Root separately uses host CUA to prove actual same-origin UI, source-down catalog error, installed packages, query/results, article/internal link, return and preserved state. Audit required resources as local; external article hyperlinks are not required assets. A source-stop host test alone cannot satisfy the actual offline scenario.
