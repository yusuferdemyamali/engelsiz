<img src="frontend/public/logo.svg" alt="Engelsiz emblem" width="72" height="72">

# Engelsiz

**Download Turkish Wikipedia, search it locally, and read without an internet connection.**

An open-source, offline-first Wikipedia reader powered by Kiwix and ZIM archives.

[Türkçe](README.md) · **English**

[![CI](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml/badge.svg?branch=main)](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Download for Linux](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.1/engelsiz-linux-x64.tar.gz) · [All releases](https://github.com/yusuferdemyamali/engelsiz/releases) · [Report a bug / suggest a feature](https://github.com/yusuferdemyamali/engelsiz/issues/new/choose)

Engelsiz keeps Wikipedia archives on your own computer. After downloading content, you can search and read articles offline. A Go application opens its embedded React interface in your default browser and serves installed content through the bundled Kiwix runtime. The current interface and supported catalog are focused on Turkish Wikipedia.

## Features

- Discover Mini, Nopic, and Maxi variants of Turkish Wikipedia, depending on upstream catalog availability.
- Pause and resume downloads; install ZIM files after size and SHA-256 verification.
- Search installed archives and read articles without an internet connection.
- Check for newer archives and explicitly start updates. The previous working version is preserved until the replacement is verified and activated.

The interface and font files are bundled locally. The application listens only on `127.0.0.1`; no account is needed to open the interface.

## Download and run

| Platform | Status |
| --- | --- |
| Linux x86_64 | [v0.1.0-alpha.1 prerelease available](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v0.1.0-alpha.1) |
| Windows x86_64 | [Installer in development](https://github.com/yusuferdemyamali/engelsiz/issues/10); no published installer yet |

Linux requires kernel 4.4+ and a desktop browser/opener. You do not need to install Go, Node, SQLite, or Kiwix separately. This is an alpha release; [full platform acceptance](docs/release-validation.md) is still in progress.

1. Download the [Linux archive](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.1/engelsiz-linux-x64.tar.gz) and its [SHA-256 file](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.1/engelsiz-linux-x64.tar.gz.sha256) into the same directory.
2. Verify, extract, and launch:

   ```sh
   sha256sum -c engelsiz-linux-x64.tar.gz.sha256
   tar -xzf engelsiz-linux-x64.tar.gz
   ./engelsiz/engelsiz
   ```

3. In **Bilgi Paketleri** (knowledge packages), choose a Wikipedia archive, download it, and install it. Use **Ara** (search) to search installed content.

Wikipedia content is not included in the application download. The initial content download needs an internet connection and enough free disk space for the selected archive. The application package is approximately 295 MiB because it also includes the runtime’s corresponding sources and licenses.

You can open the local address printed in the terminal manually. The preferred port is `17321`; if occupied, the app chooses another available loopback port. Press `Ctrl+C` in the terminal to stop the application.

## Local data

On Linux, content and the database default to `~/.local/share/engelsiz`, configuration to `~/.config/engelsiz`, and cache to `~/.cache/engelsiz`. The `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, and `XDG_CACHE_HOME` overrides are supported. Removing the extracted program directory preserves this user data.

## Contribute

Bug reports, installation tests, documentation, translations, and code contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for local development and test commands. Reports can be written in Turkish or English.

- [Product scope and technical requirements, in Turkish](docs/main-spec.md)
- [Release validation and platform acceptance](docs/release-validation.md)
- [Packaging and third-party sources](third_party/README.md)
- [Open work and roadmap](https://github.com/yusuferdemyamali/engelsiz/issues)
- [Long-term vision, in Turkish](docs/vision.md): other open knowledge sources, maps, education, and local AI are future directions, beyond the current alpha’s feature set.

## License and acknowledgements

Engelsiz’s own code is licensed under [MIT](LICENSE). Kiwix, its dependencies, and downloaded content retain their respective licenses. The distributed runtime’s corresponding sources and original notices are included under `LICENSES` in the package. See [third-party information](third_party/README.md).

Built with the work of [Kiwix](https://kiwix.org/), [openZIM](https://openzim.org/), and Wikipedia contributors.

> Keep knowledge with you before you need it.
