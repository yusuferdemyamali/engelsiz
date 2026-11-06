<img src="frontend/public/logo.svg" alt="Engelsiz emblem" width="72" height="72">

# Engelsiz 1.0.0

**Download Wikipedia, maps and learning resources; use them offline on your device.**

An offline-first knowledge hub with Turkish Wikipedia, Türkiye maps, translation, education and a fully local AI assistant. Go + React; Windows and Linux.

[Türkçe](README.md) · **English**

[![CI](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml/badge.svg?branch=main)](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/yusuferdemyamali/engelsiz)](https://github.com/yusuferdemyamali/engelsiz/releases/latest)

[Download for Windows](https://github.com/yusuferdemyamali/engelsiz/releases/download/v1.0.0/Engelsiz-Setup.exe) · [Download for Linux](https://github.com/yusuferdemyamali/engelsiz/releases/download/v1.0.0/engelsiz-linux-x64.tar.gz) · [All releases](https://github.com/yusuferdemyamali/engelsiz/releases) · [Report a bug / suggest a feature](https://github.com/yusuferdemyamali/engelsiz/issues/new/choose)

**Stable release: `1.0.0`.** All modules, including the local AI assistant, are available in one application. AI models and the inference engine are optional; Engelsiz works without them. [Release notes](docs/releases/1.0.0.md) · [Assistant setup](docs/offline-ai.md) (Turkish).

Engelsiz keeps the resources you choose on your own computer. Installed content opens without a connection. A Go application opens its locally bundled React interface in your default browser; the bundled Kiwix runtime serves ZIM archives. PDFs and verified HTML5 learning resources also open inside the application. The interface is in Turkish.

## Features

- **Wikipedia:** choose Mini, Nopic and Maxi archives, subject to upstream availability; pause/resume transfers and verify size/SHA-256 before installation.
- **Search and read:** browse installed ZIMs offline and explicitly start updates. The previous working version is preserved until its replacement is verified.
- **Maps:** download any of the 81 provinces independently; zoom, Turkish labels and attribution work locally.
- **Translation:** optional models provide Turkish ↔ English translation through the local Argos engine; text stays on your device.
- **Education:** discover, download, open and remove resources in Disaster & First Aid, Survival & Practical Skills, Mathematics & Science, and Interactive Science Lab. Supports PDFs, the Turkish Wikibooks ZIM and three Turkish PhET simulations.
- **AI Assistant (1.0.0):** optional Qwen3 GGUF models and llama.cpp provide local Turkish answers with genuine installed-source references. No AI downloads occur on first launch. See [verification and hardware limitations](docs/offline-ai-verification.md).

The interface and font files are bundled locally. The application listens only on `127.0.0.1`; no account is needed to open the interface.

## Download and run

| Platform | Status |
| --- | --- |
| Linux x86_64 | [1.0.0 archive and SHA-256](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v1.0.0) |
| Windows x86_64 | [1.0.0 installer and SHA-256](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v1.0.0) |

You do not need to install Go, Node, SQLite, or Kiwix separately. The Linux core requires kernel 4.4+ and a desktop browser/opener. The optional AI engine requires glibc 2.35+ and translation requires glibc 2.38+. Windows engines may require the Microsoft VC++ x64 runtime. Windows 10, GPU and low-memory coverage are documented in [validation and known limitations](docs/release-validation.md); a stable version label does not mean every hardware configuration has been tested.

### Windows

1. Download [Engelsiz-Setup.exe](https://github.com/yusuferdemyamali/engelsiz/releases/download/v1.0.0/Engelsiz-Setup.exe) and its [SHA-256 file](https://github.com/yusuferdemyamali/engelsiz/releases/download/v1.0.0/Engelsiz-Setup.exe.sha256). In PowerShell, compare `Get-FileHash .\Engelsiz-Setup.exe -Algorithm SHA256` with the checksum file.
2. Run setup and launch Engelsiz from the Start menu. Administrator privileges and separate Go, Node, SQLite, or Kiwix installs are not required.
3. Download and install an archive in Bilgi Paketleri, then use Ara to search. Pick a province in Haritalar to download a map and open it offline with Haritayı aç. Uninstalling preserves downloaded content and settings.

### Linux

1. Download the [Linux archive](https://github.com/yusuferdemyamali/engelsiz/releases/download/v1.0.0/engelsiz-linux-x64.tar.gz) and its [SHA-256 file](https://github.com/yusuferdemyamali/engelsiz/releases/download/v1.0.0/engelsiz-linux-x64.tar.gz.sha256) into the same directory.
2. Verify, extract, and launch:

   ```sh
   sha256sum -c engelsiz-linux-x64.tar.gz.sha256
   tar -xzf engelsiz-linux-x64.tar.gz
   ./engelsiz/engelsiz
   ```

3. In **Bilgi Paketleri** (knowledge packages), choose a Wikipedia archive, download it, and install it. Use **Ara** (search) to search installed content. In **Haritalar**, search a province to download its map (roughly 7–80 MB per province) and view it with no connection.

Wikipedia content and AI models are not included in the application download. Initial content/model downloads need an internet connection and enough free disk space. Packages include Kiwix's corresponding sources and licenses; current file sizes are shown on the release page.

You can open the local address printed in the terminal manually. The preferred port is `17321`; if occupied, the app chooses another available loopback port. Press `Ctrl+C` in the terminal to stop the application.

## Local data

On Linux, content and the database default to `~/.local/share/engelsiz`, configuration to `~/.config/engelsiz`, and cache to `~/.cache/engelsiz`. The `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, and `XDG_CACHE_HOME` overrides are supported. Removing the extracted program directory preserves this user data.

## Fully local AI assistant

1. Open **Asistan → Motoru indir** to install the CPU engine. Internet is needed only for the download.
2. Download and activate one model: **Lite**, about 397 MB / 4 GB RAM target; **Balanced**, 1.1 GB / 8 GB; **Quality**, 2.5 GB / 12-16 GB. These are targets, not hardware guarantees.
3. Ask a Turkish question about an installed source such as Wikipedia or Wikibooks. Select **Durdur** to stop streaming and use the local source links to read the original text.

Questions and sources stay on your device; no cloud API is used. Other modules work without AI. Each question is independent and visible chat history is memory-only. Lite can give incomplete or incorrect answers; check the original source. [Model provenance, licensing and limitations](docs/offline-ai.md) (Turkish).

## Offline Education Center

The **Eğitim** tab contains 14 entries: 8 downloadable resources and 6 reference-only links. Starter content includes three WHO PDFs, OpenStax Anatomy & Physiology, Turkish Wikibooks and three Turkish PhET simulations. Cards show source, language, size, version, license and download state; downloading the entire collection is never required.

1. Choose a category or search; check the language and terms of use.
2. Select **İndir** (download). Completion while the page is open starts verified installation; otherwise select **Kur** (install). Transfers can be paused or retried.
3. Select **İçeriği aç** (open) to use the local PDF/ZIM/HTML5 reader. Content remains available after restarting without internet. **Sil** (delete) removes only the selected resource.

**Licensing and scope:** WHO and PhET materials have noncommercial-use restrictions; Engelsiz's MIT license does not override them. AFAD, the health ministry, MEB, iFixit and CDC entries without verified redistribution/display suitability remain external references, not offline content. Official Turkish disaster/first-aid coverage is incomplete. WHO SH+ is a stress-management course, not a first-aid manual. Kolibri requires a separate service and is not integrated.

[Verified sources, licensing evidence and network-isolated acceptance report](docs/offline-education-center.md) (Turkish)

## Offline translation

**Çeviri** (Translation) is implemented on `main`; package availability is stated above.

1. Open **Çeviri** and select **İndir** for the direction you need: approximately 115 MiB for Turkish → English or 119 MiB for English → Turkish. Each direction is independent; downloads start only on your action.
2. Installation follows completion while this page is open. If you leave it, return and select **Kur**. Existing download pause/resume/retry controls remain available.
3. Type or paste up to 5,000 Unicode characters and select **Çevir** or press `Ctrl+Enter`. Use **Yönü değiştir** to swap direction and **Sonucu kopyala** to copy the result.

Installed models work offline. Text stays in the local Argos process and is not logged or saved as history. The native distribution bundles Python and engine dependencies under `runtime/translation`; no user Python/pip setup is needed. Models live under `content/translation` in the normal application data directory. The main app, Kiwix and PMTiles are fully static and need no glibc; the offline translation worker requires glibc 2.38+ (Ubuntu 23.04+/Debian 13+/Fedora 38+ era distros). See [model provenance and licensing limitations](docs/translation-models.md) and the [technical delivery/test report](docs/offline-translation-delivery.md).

## Contribute

Bug reports, installation tests, documentation, translations, and code contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for local development and test commands. Reports can be written in Turkish or English.

- [Product scope and technical requirements, in Turkish](docs/main-spec.md)
- [Release validation and platform acceptance](docs/release-validation.md)
- [Offline Education Center: sources, licensing and tests](docs/offline-education-center.md)
- [Packaging and third-party sources](third_party/README.md)
- [Open work and roadmap](https://github.com/yusuferdemyamali/engelsiz/issues)
- [Long-term vision, in Turkish](docs/vision.md): more open learning resources and local AI. The 1.0.0 assistant has a [separate scope document](docs/offline-ai.md).

## License and acknowledgements

Engelsiz’s own code is licensed under [MIT](LICENSE). Kiwix, its dependencies, and downloaded content retain their respective licenses. The distributed runtime’s corresponding sources and original notices are included under `LICENSES` in the package. See [third-party information](third_party/README.md).

Built with the work of [Kiwix](https://kiwix.org/), [openZIM](https://openzim.org/), and Wikipedia contributors.

> Keep knowledge with you before you need it.
