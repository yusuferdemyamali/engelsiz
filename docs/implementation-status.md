# Engelsiz 1.0.0 implementation status

Updated: 2026-11-06. Current branch: `main`. Current channel: **stable 1.0.0**. A stable product version and a compiled binary do not imply every clean-machine/hardware configuration has been tested. [Release notes and publication evidence](releases/1.0.0.md).

| Area | Implemented state | Publication / remaining validation |
| --- | --- | --- |
| Wikipedia / ZIM | Discovery, resumable download, SHA-256 install, local Kiwix search/reader and safe updates | Included in 1.0.0; full clean-platform matrix remains open |
| Offline maps | 81 province packages, local labels/glyphs/sprites and viewer | Included in 1.0.0 |
| Offline translation | Independent Turkish ↔ English models; bundled Argos worker; no cloud fallback | Included in 1.0.0; new installer is covered by the release workflow's native Windows QA |
| Offline Education Center | Four categories, pinned local manifest, download/install/open/delete, PDF, Turkish Wikibooks through existing Kiwix, three Turkish PhET HTML5 simulations | Included in 1.0.0; real download/install and network-isolated restart evidence in module report |
| Local AI assistant | Optional Qwen3 GGUF profiles, verified llama.cpp engine installation, CPU inference, bounded local ZIM/HTML RAG, streaming/cancel and real sources | Included in 1.0.0; real Linux engine validated and user confirmed Lite chat. Windows AI/GPU/physical RAM matrix remains open |
| Source/license gates | Existing reviewed Kiwix/PMTiles inputs, current Go/frontend notices; third-party learning payloads never bundled | Eight downloadable education entries and six reference-only entries; WHO/PhET noncommercial conditions remain in force |
| Automated verification | Full Go/race/frontend/packaging checks and exact-toolchain notices; counts and commands in the linked reports | Native release workflow and provenance bind artifacts to the exact tested commit; Wine/cross-build do not establish Windows AI acceptance |
| Future / incomplete | Broader official Turkish first-aid resources; Kolibri runtime/channels; AI PDF text extraction and private-document RAG | Not implemented or not lawfully verified; do not list as working features |

## Evidence and boundaries

- [Education implementation, sources, licensing and real offline evidence](offline-education-center.md).
- [Local AI architecture and setup](offline-ai.md), [AI verification evidence and limits](offline-ai-verification.md).
- [Translation delivery](offline-translation-delivery.md) and [model provenance](translation-models.md).
- [Release/platform acceptance matrix](release-validation.md), [packaging review](packaging-review.md) and [Windows evidence](windows-installer-validation.md).
- [Current public releases](https://github.com/yusuferdemyamali/engelsiz/releases); each release's own asset list/provenance determines what was actually published.

Engelsiz's own code is MIT licensed. Runtime dependencies and downloaded content keep their own licenses. Earlier source-candidate results remain evidence for their original candidate, not automatic validation of every subsequent package.
