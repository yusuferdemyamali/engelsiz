# Implementation checkpoint

Updated: 2026-11-06. Current branch: `main`. Feature implementation and publication are tracked separately; a compiled binary is not clean-machine acceptance evidence.

| Area | Implemented state | Publication / remaining validation |
| --- | --- | --- |
| Wikipedia / ZIM | Discovery, resumable download, SHA-256 install, local Kiwix search/reader and safe updates | Included in existing alpha releases; full clean-platform matrix remains open |
| Offline maps | 81 province packages, local labels/glyphs/sprites and viewer | Published in `v0.1.0-alpha.3` |
| Offline translation | Independent Turkish ↔ English models; bundled Argos worker; no cloud fallback | Implemented on `main`; new application packages are being prepared. `alpha.4` currently hosts a CI compliance input, not installers |
| Offline Education Center | Four categories, pinned local manifest, download/install/open/delete, PDF, Turkish Wikibooks through existing Kiwix, three Turkish PhET HTML5 simulations | Commit `25e74f7`; 7 real downloads/installations and network-isolated cold restart verified. New `alpha.5` packages are being prepared |
| Source/license gates | Existing reviewed Kiwix/PMTiles inputs, current Go/frontend notices; third-party learning payloads never bundled | Eight downloadable education entries and six reference-only entries; WHO/PhET noncommercial conditions remain in force |
| Automated verification | 486 Go tests, 324 race tests and 39 frontend tests passed; vet/build passed; Python 52 passed / 3 Argos-runtime-dependent tests skipped | Linux development evidence; Windows cross-compilation is not native installer acceptance |
| Future / incomplete | Broader official Turkish first-aid resources; Kolibri runtime/channels; local AI Q&A | Not implemented or not lawfully verified; do not list as working features |

## Evidence and boundaries

- [Education implementation, sources, licensing and real offline evidence](offline-education-center.md).
- [Translation delivery](offline-translation-delivery.md) and [model provenance](translation-models.md).
- [Release/platform acceptance matrix](release-validation.md), [packaging review](packaging-review.md) and [Windows evidence](windows-installer-validation.md).
- [Current public releases](https://github.com/yusuferdemyamali/engelsiz/releases); each release's own asset list/provenance determines what was actually published.

Engelsiz's own code is MIT licensed. Runtime dependencies and downloaded content keep their own licenses. Earlier source-candidate results remain evidence for their original candidate, not automatic validation of every subsequent package.
