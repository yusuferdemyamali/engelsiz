## Source and extraction check, 2026-11-02

- The default source returned HTTP 206 for `Range: bytes=0-126`, with `Content-Range: bytes 0-126/139247504393`. Its PMTiles v3 header reported MVT tiles, zoom 0–15.
- Eskişehir province OSM relation 223421 was looked up through Nominatim. The returned bbox was 29.9883672,39.0349197,32.0115423,40.1872590 (west,south,east,north).
- Official `pmtiles` 1.31.2 for Linux x86-64 extracted that bbox at max zoom 15. The tool reported 136 requests, 28 MB transferred and a 27 MB archive. The temporary output passed `pmtiles verify`; `pmtiles show --header-json` reported PMTiles MVT and zoom 0–15 with the requested bounds.
- This was a standalone CLI experiment, not an application-managed installation. It did not verify rendered detail, Turkish glyphs, offline operation, cancellation, Windows packaging or restart persistence. The test archive and CLI are temporary and are not shipped.

## Backend continuation, 2026-11-03

- Inspected the provider-owned external-operation adapter, app registration, trusted-source configuration, migration 005 and startup recovery. Regional extraction is not passed to the generic HTTP transfer worker. Existing generic download and API suites pass.
- `go test -race -count=1 -v ./internal/providers/maps/` passed all 12 tests; `go test -count=1 ./internal/download/... ./internal/api/...` and `go test -race -count=1 ./internal/download/` passed. These are focused results, not the final full-repository run.
- Added real HTTP tests driving `MapsHandler` with a controlled HTTPS source and injected extraction runner: list, download, retry, duplicate 409, full-file 200, HEAD, Range 206/416, deletion 204, unknown 404 and active-deletion 409. A 16-goroutine start test accepts exactly one operation. Existing tests cover failed-extraction cleanup, interrupted recovery, ready-state restart and restoring the file after a database deletion failure.
- Lifecycle test archives are synthetic; these tests do not prove real extraction or rendered map quality. Full-app request protections remain covered separately by the existing API tests.
- The concurrency test holds regional extraction active while a generic Kiwix transfer completes with the expected byte count; one manager listing shows the map still downloading and the generic transfer completed. Releasing extraction then installs the map. A separate test verifies retry after failure through the download manager.
- A non-destructive offline probe succeeded with `bwrap --unshare-net --dev-bind / /`: external TCP is unreachable while loopback remains usable. A real app booted inside this namespace and served `/`, `/api/health` and `/api/maps`. **That state had no installed map**, so this is not acceptance of offline rendering. `unshare -n` is denied; `unshare -U -n` has unusable loopback here.
- Temporary commands/logs are in `/tmp/opencode/offline-maps-backend-evidence.md` and `/tmp/opencode/offline-app/`; they are not release assets and are not committed.

## Distribution and asset audit, 2026-11-03

- Downloaded the official PMTiles 1.31.2 Linux and Windows x64 archives and matched both complete SHA-256 pins in `third_party/pmtiles-lock.json`: Linux `3ed7dbf4ec2e6dfe5e25b6f70d1ffc932729f93c86db353bf514dd71010a312f`, Windows `a658baa4d7e55020aef6ca17bd9ff9faa1582671266b36f58c52db0ac8e785a1`.
- Exercised the release extraction primitive on the real archives: `runtime/pmtiles/pmtiles` and `runtime/pmtiles/pmtiles.exe` plus upstream LICENSE/README are staged. Linux PMTiles passes the static-ELF check and reports version 1.31.2; the Windows executable passes PE dependency inspection (only kernel32.dll). A test tarball retains executable mode 0755. **This is component staging, not a complete distributable or a native Windows acceptance test.**
- `python -m unittest discover -s scripts/tests -v` passed all 15 tests, including notice-pin enforcement and the PMTiles executable archive mode.
- Reproduced `collect-pmtiles-notices.py --check` for both real archives using the exact Go 1.26.1 toolchain selected with `GOTOOLCHAIN=go1.26.1 go env GOROOT`. Both generated notice files match, including the source-module and binary dependency-graph checks.
- `python scripts/collect-app-notices.py --go "$(go env GOROOT)/bin/go" --check` passed with the exact release Go 1.26.0 executable: frontend notice hash `46683e6a212c66588f36baec9b29aa3f36a60197f76aaf253c4e8fd332221d2d`, Go notice hash `b7b70ab9edbff358c84ddb02036bb1f988fa0f4778aa468f57dd1a6e4bbbe065`. A launcher selected through PATH reports 1.25.4 when the collector forces `GOTOOLCHAIN=local`; that initial toolchain-selection failure was resolved by passing the actual GOROOT executable, without changing notices.
- The bundled glyph/sprite source is `protomaps/basemaps-assets` commit `028c18f713baecad011301ff7a69acc39bcc2ae7`. All four dark sprite files, OFL text and sampled Latin/Turkish glyph ranges match upstream bytes. Glyphs include ranges 0–255 and 256–511 for ÇçÖöÜüĞğİıŞş; visual acceptance is separate. OFL-1.1 covers fonts and MIT Mapzen attribution covers the Tangram-derived sprites.
- `frontend/public/maps`, `frontend/dist/maps` and `internal/web/dist/maps` matched recursively during the audit. `//go:embed all:dist` carries them inside the Go executable. The final frontend build and binary checks must re-establish this for final artifacts.

### Full release remains blocked (do not bypass)

- `third_party/runtime-lock.json` has `complianceManifestSha256: null` for both official Kiwix runtimes; `third_party/compliance/` is absent. The release gate rejects the missing reviewed pin.
- Existing ignored candidate manifests are internally intact but bind obsolete `frontend.txt` hashes (`4f63aacc…` Linux, `fa66fbd1…` Windows versus current `46683e6a…`). The gate rejects them with `Bundled frontend attribution is stale for the current application notice inventory`.
- Linux candidate sources are reusable review inputs, not proof of correspondence to a different archive. Windows candidate sources are for source-built 3.8.2, whereas the official Windows pin is 3.8.1 with different dependencies; they cannot approve that binary. `third_party/runtime-source-candidates/windows-r91.json` records remaining official-Windows review blockers.
- A fresh complete-source/compliance review must bind the exact shipped runtime and current application notices before either full release can pass. No reviewed manifest, human approval or runtime lock was rewritten to bypass this.
- Detailed temporary audit: `/tmp/opencode/offline-maps-release-evidence.md`; staging/downloads: `/tmp/opencode/offline-maps-stage/`. No temporary artifact is committed. The packaging task stays unchecked until the complete distribution is established.

## MapLibre blank-map fix and visual verification, 2026-11-03

- Captured the underlying error before the fix: `Worker failed to load. Check that the worker URL is correct.` MapLibre 6 requested `/assets/maplibre-gl-worker.mjs?v=6.16.0`, but Vite had not emitted that dynamically resolved file. The SPA fallback returned HTML with 200, preventing worker startup, vector-tile parsing and glyph requests. This was a worker-asset packaging issue, not a failed PMTiles Range response.
- `frontend/src/maps.tsx` now imports `maplibre-gl/dist/maplibre-gl-worker.mjs?url` and assigns `maplibregl.config.WORKER_URL`. The emitted hashed worker is local and self-contained; renderer errors retain their real detail in the console and contextual alert. No dependency downgrade or visual redesign was needed.
- Installed the real Eskişehir archive through the UI's **İndir** button: 26,833,488 bytes, PMTiles v3/MVT, Z0–Z15, 34,036 tiles. Restart preserved `ready`. The whole planet was not downloaded.
- Desktop 1440×900 and narrow 390×844 renders show a filled map canvas rather than the original top-left fragment. An independent visual inspection confirms roads, landcover, water and route shields, plus urban street/neighborhood detail in `shot-city4-z15.png`. Turkish labels include Söğüt, İnönü, Mihalıççık, Günyüzü, Söğütlü Çayı, YEŞİLTEPE and SÜTLÜCE without broken characters. Initial screenshots crop the map bottom; attribution visibility needs a full-canvas capture rather than inference from those crops.
- Browser interaction checks covered drag/pan, wheel zoom and keyboard zoom. A direct local PMTiles probe returned data for Z8–Z15. A sparse rural center tile at Z15 is genuinely near-empty, so urban detail was checked separately. Some low-zoom upstream place data spell `Eskisehir` in ASCII; other map labels and urban Turkish names render diacritics correctly.
- First genuine offline restart placed **both backend and fresh Chromium in the same network namespace** using `bwrap --unshare-net --dev-bind / /`. External TCP to 8.8.8.8:53 and build.protomaps.com:443 was unreachable, loopback remained available, and the previously installed archive rendered at overview and another zoom. Network logs record 60 local 200/206 responses, glyphs including 256–511, sprites and tiles, no external browser requests and no MapLibre errors. A catalog 503 is the pre-existing remote Kiwix catalog being unavailable, not a map request failure. This isolates the whole tested app/browser network stack; it does not disable the user's host networking.
- Temporary visual/network evidence: `/tmp/opencode/offline-maps-viewer-evidence.md` and `/tmp/opencode/offline-maps-check/` (`shot-viewer-desktop.png`, `shot-city4-z15.png`, `shot-viewer-narrow.png`, `ns-shot-overview.png`, `ns-shot-zoomed.png`, `ns-{console,requests,responses}.txt`). The expanded acceptance and final current-code results follow below.

## Final current-code checks, 2026-11-03

After reconciling the backend, frontend and distribution lanes:

- `bash scripts/build-frontend.sh` passed (TypeScript + Vite, then copied the complete output into `internal/web/dist`). The 519 KB hashed MapLibre worker is present. Vite reports the expected large lazy map chunk warning; it is not a build failure.
- `node --test frontend/tests/*.test.mjs` passed **10/10** tests, including the new production-bundle regression test: the actual map entry must emit a worker asset containing executable code and reference its hashed local URL. Removing the worker wiring must not silently produce the original broken bundle. The test-only reduced Vite configuration emits a Tailwind content warning; the full production build above does not.
- `go test -count=1 ./...` passed every package, including migration count/table assertions and existing app, Kiwix, packages, downloads and API regression tests. `go vet ./...` passed.
- `go test -race -count=1 ./internal/app ./internal/packages ./internal/download ./internal/runtime/kiwix ./internal/providers/maps ./internal/api` passed all six packages.
- `npm audit --prefix frontend --json` reports **0 vulnerabilities** across all severities. `python -m unittest discover -s scripts/tests -v` again passed **15/15** tests.
- Built both targets with `CGO_ENABLED=0`, `GOARCH=amd64`, `GOAMD64=v1`, `-trimpath -buildvcs=false`: `GOOS=linux` and `GOOS=windows` both passed. The Windows result is a cross-build, not native Windows installation or rendering acceptance.
- Commands/logs and uncommitted test executables: `/tmp/opencode/offline-maps-final/` (`frontend-build.log`, `frontend-tests.log`, `go-test.log`, `go-vet.log`, `go-race.log`, `npm-audit.json`, `packaging-tests.log`, `engelsiz-linux`, `engelsiz-windows.exe`). These are development validation artifacts, not approved releases; the compliance blocker above is unchanged.
- Booted the final Linux binary in an isolated network namespace and fetched **all 1,031 map asset files (17,769,118 bytes)** through its HTTP server; every file byte-matches `frontend/public/maps`. Fetched the hashed worker and confirmed exact bytes and a non-HTML content type. Linux binary passes the static-ELF gate. All 1,031 map files plus the worker also occur byte-for-byte inside the final Windows PE binary (1,032 resources, none missing); its only PE import is kernel32.dll. This is direct Linux serving evidence and static Windows embedding evidence, not native Windows execution. Results: `embedded-smoke.json`, `windows-embedded-resources.json`.
- Final development binary SHA-256: Linux `c08314690d51a80147790580878df8c6faed2cae270b70ca57d386f3030a1763`; Windows `af6f5b1d6e1583a1b795238dc1496c912efecf53495cbf45954b11237b35f15f`.
- Strict OpenSpec change validation passed with no issues. `git diff --check` reports two trailing-whitespace lines in verbatim upstream MurmurHash attribution (`third_party/application-notices/frontend.txt`); those authenticated license/notice bytes were deliberately preserved. The generated-notice verification itself passes.

## Full distribution builds and package validation, 2026-11-03

The compliance blocker is resolved through the maintainer-selected
**source-built** runtime route, not by approving the official binaries:

- `third_party/runtime-source-candidates/offline-maps/` holds per-platform
  candidate locks plus refreshed compliance manifests and a route README. The
  Linux manifest (`2e8353cd…`) and Windows manifest (`ed93ec51…`) keep all
  original component entries byte-identical and only refresh the
  `applicationDependencies` pins to the current authenticated notices (Go
  `b7b70ab9…`, frontend `46683e6a…`). Review provenance is recorded honestly
  in-manifest; no human sign-off is invented. `release.compliance()` and the
  PMTiles 1.31.2 notice gates pass unmodified on both refreshed bundles.
- `scripts/release.py` now stages readable map-asset notices under
  `LICENSES/map-assets/` (`README.txt`, `fonts/OFL.txt`, `sprites/LICENSE.txt`,
  byte-identical to the shipped frontend tree) with focused regression tests.
- Full `release.py` builds ran with Node 22.23.4, Go 1.26.0 and Python 3.14:
  `dist/offline-maps-package/linux-amd64/release/engelsiz-linux-x64.tar.gz`
  (`e40c5aaa…`, 333,409,335 bytes) and
  `dist/offline-maps-package/windows-amd64/release/Engelsiz-Setup.exe`
  (`85e6f341…`, 265,080,640 bytes, Inno Setup 6.5.4 compile 81 s).
- Linux package validation (fresh Turkish/space extraction path): app binary,
  `runtime/kiwix-serve` (reports 3.8.2) and `runtime/pmtiles/pmtiles` (reports
  1.31.2) all pass the static-ELF gate; tarball keeps 0755 on all three
  executables. Extracted-app smoke with isolated XDG state and **no binary
  overrides** served the hashed worker, Turkish glyph range 256–511 and dark
  sprite as 200. POST `/api/maps/eskisehir/download` through the packaged app
  went `available → downloading → ready` in ~30 s and produced
  `eskisehir.pmtiles` SHA-256 `68ba4a36…` (26,833,488 bytes), byte-identical
  to the development-state archive; `bytes=0-126` → 206 and an unsatisfiable
  range → 416. Stopped, then restarted the **packaged** app with its state
  inside `bwrap --unshare-net`: external TCP unreachable, loopback fine,
  `/api/maps` still `ready`, file 206 and worker/glyph/sprite all local.
- Windows package validation under Wine 11 (isolated prefix, not clean native
  Windows): the installer compiled from this build installed silently
  (exit 0) with `engelsiz.exe`, `runtime/pmtiles/{pmtiles.exe,LICENSE,
  README.md}` and `LICENSES/` present. The installed app booted on a fallback
  loopback port, reported health ok, listed Eskişehir as `available`, and
  served the worker, glyph and sprite ranges as 200. Map acquisition through
  the installed Windows app (exercising the staged `pmtiles.exe` under Wine)
  completed: `available → downloading → ready` with `sizeBytes` 26,833,488;
  the installed `eskisehir.pmtiles` is SHA-256 `68ba4a36…`, byte-identical to
  the Linux-package archive, and serves 206 with correct Content-Range.

Isolated test processes were stopped after verification. `dist/` build output
is ignored by Git; the committed route records are the candidate locks,
manifests and README above.

## Expanded genuine offline acceptance, 2026-11-03

- Reused the UI-installed Eskişehir archive after shutting down the online app and starting the app plus a fresh Chromium inside `bwrap --unshare-net`. Negative socket controls assert that 8.8.8.8:53 and build.protomaps.com:443 cannot be reached; the host network was left untouched. No browser interception or invalid-proxy substitute was used.
- `ns_city.py` pans into urban Eskişehir, reaches the viewer's Z15 limit, pans twice to different city positions and zooms back out. It also opens a fresh 390×844 context in the same isolated namespace. Tests fail on external requests, MapLibre/page errors or a map error alert. The local PMTiles index probe (`ns_tileproof.mjs`) matches six browser-requested city Z15 tile ranges to the archive index (example Z15/19161/12430: 112,914 bytes), establishing actual Z15 data rather than merely overzoomed lower-resolution tiles.
- Parent independently parsed all **171 recorded browser request URLs** by hostname: every request is loopback, with **zero external requests**. All recorded map/worker responses are 200 or 206. Logs and assertions are in `ns2-{requests,responses,ranges,console,backend}.txt`/`.log`; the only expected non-map error is the unavailable remote Kiwix catalog.
- Independent visual review confirms rich street/POI detail and different panned city positions in `ns2-city-z15a.png` and `ns2-city-z15b.png`, readable Turkish labels, full-canvas rendering and a sound narrow layout. The dark airport polygon is the inherited upstream `landuse_aerodrome` fill with a lighter runway, not missing tiles.
- `ns_attrib.py` asserts `© OpenStreetMap · Protomaps` in the attribution control for desktop and narrow contexts. Full-map element screenshots `ns3-map-desktop.png` and `ns3-map-narrow.png` were independently reviewed: the credit is visible and legible at the bottom-right; the earlier cropped screenshots are no longer the only evidence.
- The installed archive's SHA-256 is `68ba4a36c73f98890c2c5deca64ffedb2cdbd9fdf1ab36249df9af5f017faaa2` (26,833,488 bytes). The UI acquisition used the development override `$HOME/go/bin/go-pmtiles`, whose Go build info identifies upstream `github.com/protomaps/go-pmtiles v1.31.2` (binary SHA-256 `8bd445ee28299b2eabd066fa4fe1448ec4bd21cb9d702926c2d6fabb567b171c`). This source-built upstream extractor is distinct from the official release binaries verified in component staging; do not confuse their hashes or imply a full packaged-release test.
- Isolated test app processes were stopped after verification. Temporary archives/screenshots/probes remain outside Git. Packaging was completed afterwards; see "Full distribution builds and package validation" above.
