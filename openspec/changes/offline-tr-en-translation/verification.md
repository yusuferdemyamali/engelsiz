# Verification evidence

Status: Linux implementation, real packaged/offline use and browser acceptance verified. Native Windows and clean desktop VM acceptance remain unverified; no pending criterion is claimed complete. [Technical delivery report](../../../docs/offline-translation-delivery.md).

## Evidence ownership

1. Backend implementer: deterministic provider, installation, API validation/concurrency/recovery tests; no full-suite duplication.
2. Runtime/packaging implementer: real native worker build, frozen-runtime smoke tests, platform-specific manifests/notices, focused packaging tests.
3. Designer: UI interaction/state tests and theme/responsive/accessibility inspection with design-taste-frontend.
4. Parent: reconciled full suites, actual running application/browser, real model acquisition, restart, and translation with networking unavailable.

## Decisive checks

- Verify both upstream packages by actual transfer and SHA-256; record provenance and model license contents.
- Request model downloads through the actual app, observe generic Download Manager status, install and restart.
- Send Turkish/English and multiline input through the loopback API; verify missing/invalid/oversize/error cases separately.
- Run app and worker in `unshare --user --map-root-user --net` namespace, prove outbound networking fails and loopback translation still succeeds. Do not disconnect the host's real interfaces.
- Measure cold load, warm translations and worker RSS from real processes. Inspect browser swap/clear/copy and responsive navigation.
- Run full Go/frontend/Python existing checks after merging disjoint work. Cross-compile Windows where possible; do not equate cross-build with Windows execution.

## Running application / browser evidence

- `bash scripts/build-frontend.sh` and `go build -o '/tmp/opencode/Engelsiz Çeviri Test/engelsiz' ./cmd/engelsiz`: exit 0. Existing MapLibre bundle-size warning remains; no new dependency was added to npm.
- Started the real application with isolated XDG roots under `/tmp/opencode/engelsiz-ui-verification`, not user data. Health check passed at `127.0.0.1:17321`; the compiled UI was served from Go embed.
- Desktop 1440×1100 screenshot `/tmp/opencode/engelsiz-translation-desktop.png` inspected by the designer: existing theme/navigation, missing-engine alert, independently missing models, disabled translate and readable controls accepted; no blocking visual defects. Observer could not run because its configured model is deprecated; no observer verdict is claimed.
- Mobile 390×844 with reduced motion: document width 390, no horizontal overflow. Both model cards and input controls appear in the accessibility snapshot. Swap changes direction; multiline Turkish input is retained; Temizle empties the field.
- Real browser `İndir` on tr→en started the existing generic transfer; `Duraklat` persisted 63.7 MB / 52.9%, `Devam et` resumed. Automatic installation completed while Çeviri remained open: model became `ready`; reverse direction stayed missing. No runtime was present during this first UI check, so translation correctly remained disabled.
- Started en→tr from the same UI and navigated to İndirmeler. Generic progress rendered and the Argos completed file showed `Çeviriye git`, not ZIM package installation. Remaining runtime/bidirectional/copy/offline checks pending below.

The final reconciled evidence section below supersedes these interim checks.

### Real engine + browser functional run

`node /tmp/opencode/engelsiz-browser-e2e.mjs http://127.0.0.1:17321`: exit 0. This session-local CDP harness used a separate temporary headless Chrome profile with clipboard permissions; it did not add a product dependency. The earlier interactive browser's clipboard-read permission prompt blocked its automation queue, so the independent profile established the remaining assertions. A native `wl-paste --no-newline` also returned the exact result copied by the actual UI.

- TR→EN: `Merhaba, nasılsın?\nİstanbul’da güzel bir gün.\n\nTeşekkür ederim.` → `Hello, how are you?\nA beautiful day in Istanbul.\n\nThank you.`
- EN→TR: `Hello, how are you?\nIt is a beautiful day in Istanbul.` → `Merhaba, nasılsın?\nİstanbul'da güzel bir gün.`
- Asserted real clipboard contents and `Kopyalandı` feedback, paste of Turkish characters, direction swap clearing stale output, clear, blank/5,001-character button guards and rendered `white-space: pre-wrap`.
- Asserted document width ≤ viewport at 1440 and 390 px. Navigated İndirmeler, Yerel Paketler, Ara, Haritalar and Çeviri.
- Captured requests during the translation cycle: exactly two `/api/translation/translate` requests at `127.0.0.1`, no remote browser request. This is not by itself the backend offline proof; network-namespace verification remains separate.
- This initial functional run used the provisional native runtime built with Python 3.11.0rc1, not a release-accepted artifact. Final runtime is being rebuilt with stable Python and complete file inventory/native notices; final offline verification will use that artifact.

Session-local evidence: `/tmp/opencode/engelsiz-browser-evidence.json` and `/tmp/opencode/engelsiz-translation-ready.png`.

### Preliminary genuine offline application test

`unshare --user --map-root-user --net bash -c 'ip link set lo up && python3 /tmp/opencode/engelsiz-translation-e2e.py offline --binary "/tmp/opencode/Engelsiz Çeviri Test/engelsiz" --root /tmp/opencode/engelsiz-ui-verification'`: exit 0. Only `lo` existed; TCP connection to `1.1.1.1:443` returned errno **101 (ENETUNREACH)**. The real Go application and its real frozen worker both ran inside that namespace. Host networking was unchanged.

Both installed directions were detected after restart without new downloads. Four real translations (two ordinary, two multiline) succeeded; empty/whitespace/unsupported-pair/5,001-character inputs were rejected. `/api/maps`, `/api/downloads`, `/api/packages` and `/api/runtime` remained responsive. App exit was 0. This preliminary run still used the provisional runtime; final stable-runtime evidence supersedes its performance values below.

Observed model-quality limitation: `Türkçe öğreniyorum.` was mistranslated as `I learn English.` by the pinned tr→en model. Retained explicitly in `docs/translation-models.md`; not hidden by a mock or cloud fallback.

## Final reconciled evidence (supersedes provisional runtime)

Implementation commit: **8e52271106dd6c2f5709df1f6afbea2739ebb2f3**, branch `feat/offline-tr-en-translation`. Parent reviewed produced source/artifacts; oracle source gate was conditional only on final-stable packaging/offline evidence, now supplied below. No main merge/push occurred.

| Claim | Final evidence | Result |
| --- | --- | --- |
| Existing backend modules preserved | `go test ./...`, `go vet ./...`; race suite across app/packages/download/kiwix/translation/api | PASS |
| UI state/actions/build | `node --test frontend/tests/*.test.mjs`, production embed, final-package CDP browser run | 24/24 PASS; 1440/390px; real clipboard/paste/swap/clear/multiline |
| Native runtime/packaging safety | Final frozen binary + real models supplied to `python3 -m unittest discover -s scripts/tests -v`; open-stdin smoke; full inventory/native closure staging | 38/38 PASS, no skips |
| Platform build | Actual Linux source-candidate release build; Windows x86_64 Go cross-build | PASS; Windows native worker/installer NOT tested |
| Genuine offline/no system Python | Actual extracted tarball in Ubuntu 22.04.5 LTS, `podman run --network none --read-only`, absent Python/pip/Go/Node and unusable developer PATH | Both directions, restart reuse, validation, other status endpoints, Origin/Host guard PASS; only `lo`, outbound TCP ENETUNREACH |

### Final runtime and archive identity

- CPython **3.11.14**, Argos **1.11.0**, CTranslate2 **4.8.5**, SentencePiece **0.2.2**, NumPy **2.4.6**. Native source-built runtime baseline glibc **2.35**; older glibc/musl and Windows execution not established.
- Worker SHA-256 `37098f82c01b57411ec93760f107a51805d0c0b6aa860b793ccc74214c152b86`.
- 206 frozen-runtime files / **359182591 bytes**; 205 non-manifest files verified by full size/hash inventory. CPython/wheel/PyInstaller/native license payloads present. Prerelease Python is rejected by the builder/stager.
- Actual archive `dist/translation-verification/release-final/engelsiz-linux-x64.tar.gz`, **446062673 bytes**, SHA-256 `be4761ab9ee0c8eb47c7f4545c7f9d5daa283d8cc1b8e3ed140538b301717f5f`. No `.argosmodel` bundled. Existing audited local Kiwix sources/notices and current unchanged Go/frontend notice text hashes passed the release gate.

### Final offline command and measurements

The static session-local acceptance helper launches the real packaged application, uses its actual loopback API, probes denied external networking, checks `/proc` child memory/cleanup and exits. It is not an application dependency or production endpoint:

```sh
podman run --rm --network none --read-only --tmpfs /tmp:rw,size=128m \
  -v "$PWD/dist/translation-verification/extracted-final/engelsiz:/paket/Engelsiz Çeviri:ro" \
  -v "$PWD/dist/translation-verification/engelsiz-ui-verification:/kullanıcı verisi:rw" \
  -v "$PWD/dist/translation-verification/verify-packaged:/verify:ro" \
  -e 'VERIFY_APP_BINARY=/paket/Engelsiz Çeviri/engelsiz' \
  -e 'VERIFY_DATA_ROOT=/kullanıcı verisi' ubuntu:22.04 /verify
```

Exit 0; `children_cleaned: true`. Both runtime and data paths included spaces/Unicode. First tr→en request **309 ms**, repeated warm request **36 ms**; en→tr initial load in same worker **148 ms**, warm **38 ms**; multiline **84–86 ms**. Worker RSS **190416 KiB** after one model and **362164 KiB** after both (8/12 threads); pure loading time was not separately instrumented. One worker PID was reused, then reaped on app shutdown.

The final extracted package also passed the browser harness again: two directions, preserved newlines, real clipboard equality, paste, swap invalidation, clear, blank/oversize guards, responsive widths and navigation. During its translation cycle exactly two local API requests were captured.

### Discovered and fixed during verification

- First aggregate Go run exposed an install-response race: fast fixture installation returned `ready` before the accepted-202 snapshot. Active `installing` now takes precedence until the worker settles; the two reproducing tests passed 20 repetitions, followed by the complete Go and race suites.
- A stable-runtime candidate used `read(8192)`, which waited for stdin EOF/full buffer. A real persistent-pipe test refuted it despite the prior EOF-based smoke passing. Replaced with bounded `readline`, added `PersistentReaderTests` and made smoke wait for each reply before closing stdin. The failing candidate was not packaged; final hash above passed all checks.
- Attribution check first picked host Go 1.25.4 because the checker intentionally forces `GOTOOLCHAIN=local`. Selecting the actual downloaded Go 1.26.0 binary resolved that; regeneration changed only the source/package graph metadata (new archive/zip + translation paths), not third-party license text hashes.

### Evidence locations and honest remaining limits

Raw logs/JSON are session artifacts under ignored `dist/translation-verification/evidence/`; source/test assertions and this record are committed. The full candidate is local, not published. Native Windows/installer, clean desktop VM, older glibc/musl, and exhaustive real Wikipedia/81-province content re-download/manual traversal remain unexecuted. Existing regression suites and navigation/status checks passed; these do not silently close pre-existing release PENDING rows. Model quality and training-data/license provenance limits are documented in the delivery report.
