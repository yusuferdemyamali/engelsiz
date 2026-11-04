## 1. Integration and provenance
- [x] Inspect instructions, existing architecture and user changes; create feature branch without modifying main.
- [x] Verify real model sources, versions, bytes and SHA-256; record unresolved model-weight licensing separately and settle runtime protocol.
- [x] Implement independent model provider through existing Download Manager and atomic installation.
- [x] Integrate persistent bounded worker, API validation, error recovery and shutdown.

## 2. Interface and distribution
- [x] Build Çeviri page/navigation with design-taste-frontend, all module states and accessible controls (browser integration still pending below).
- [x] Bundle native Argos runtime/dependencies in Linux and Windows packaging paths; preserve notices/licenses (Linux native artifact verified; Windows native execution remains unverified).
- [x] Update developer/user documentation and relevant main specification.

## 3. Evidence and delivery
- [x] Run focused provider/worker/API/UI and packaging tests, including corrupt downloads and restart.
- [x] Reconcile all writers; run existing full checks and regression tests.
- [x] Run real model download/install and bidirectional translation through the application.
- [x] Demonstrate actual network-isolated translation; record latency/memory and platform limits.
- [x] Commit feature changes and produce technical delivery report; do not merge/push main.

## 4. Explicitly unverified platform acceptance (not a release claim)
- [ ] Execute native frozen worker and installer/uninstall/reinstall on clean Windows 10/11 x86_64 with Unicode/space-containing user paths.
- [ ] Complete a separate clean Linux desktop VM acceptance run; container/headless acceptance does not close the existing release matrix.
