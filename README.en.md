<img src="frontend/public/logo.svg" alt="Engelsiz emblem" width="72" height="72">

# Engelsiz

**Download Turkish Wikipedia and Türkiye maps, and use them offline on your device.**

An open-source, offline-first Wikipedia reader powered by Kiwix and ZIM archives.

[Türkçe](README.md) · **English**

[![CI](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml/badge.svg?branch=main)](https://github.com/yusuferdemyamali/engelsiz/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[Download for Windows](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.3/Engelsiz-Setup.exe) · [Download for Linux](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.3/engelsiz-linux-x64.tar.gz) · [All releases](https://github.com/yusuferdemyamali/engelsiz/releases) · [Report a bug / suggest a feature](https://github.com/yusuferdemyamali/engelsiz/issues/new/choose)

Engelsiz keeps Wikipedia archives on your own computer. After downloading content, you can search and read articles offline. A Go application opens its embedded React interface in your default browser and serves installed content through the bundled Kiwix runtime. The current interface and supported catalog are focused on Turkish Wikipedia.

## Features

- Discover Mini, Nopic, and Maxi variants of Turkish Wikipedia, depending on upstream catalog availability.
- Pause and resume downloads; install ZIM files after size and SHA-256 verification.
- Search installed archives and read articles without an internet connection.
- Download maps for each of the 81 provinces and view them offline: search a province in Haritalar, download it, then open it with no connection. Zoom, Turkish labels and attribution are fully local.
- Check for newer archives and explicitly start updates. The previous working version is preserved until the replacement is verified and activated.

The interface and font files are bundled locally. The application listens only on `127.0.0.1`; no account is needed to open the interface.

## Download and run

| Platform | Status |
| --- | --- |
| Linux x86_64 | [v0.1.0-alpha.3 prerelease available](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v0.1.0-alpha.3) |
| Windows x86_64 | [v0.1.0-alpha.3 installer available](https://github.com/yusuferdemyamali/engelsiz/releases/tag/v0.1.0-alpha.3) |

The project owner tested the Windows package on Windows 11 x86_64; Windows 10 has not yet been tested. Linux requires kernel 4.4+ and a desktop browser/opener. You do not need to install Go, Node, SQLite, or Kiwix separately. This is an alpha release; [full platform acceptance](docs/release-validation.md) is still in progress.

### Windows

1. Download [Engelsiz-Setup.exe](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.3/Engelsiz-Setup.exe) and its [SHA-256 file](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.3/Engelsiz-Setup.exe.sha256). In PowerShell, compare `Get-FileHash .\Engelsiz-Setup.exe -Algorithm SHA256` with the checksum file.
2. Run setup and launch Engelsiz from the Start menu. Administrator privileges and separate Go, Node, SQLite, or Kiwix installs are not required.
3. Download and install an archive in Bilgi Paketleri, then use Ara to search. Pick a province in Haritalar to download a map and open it offline with Haritayı aç. Uninstalling preserves downloaded content and settings.

### Linux

1. Download the [Linux archive](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.3/engelsiz-linux-x64.tar.gz) and its [SHA-256 file](https://github.com/yusuferdemyamali/engelsiz/releases/download/v0.1.0-alpha.3/engelsiz-linux-x64.tar.gz.sha256) into the same directory.
2. Verify, extract, and launch:

   ```sh
   sha256sum -c engelsiz-linux-x64.tar.gz.sha256
   tar -xzf engelsiz-linux-x64.tar.gz
   ./engelsiz/engelsiz
   ```

3. In **Bilgi Paketleri** (knowledge packages), choose a Wikipedia archive, download it, and install it. Use **Ara** (search) to search installed content. In **Haritalar**, search a province to download its map (roughly 7–80 MB per province) and view it with no connection.

Wikipedia content is not included in the application download. The initial content download needs an internet connection and enough free disk space for the selected archive. The packages include the runtime’s corresponding sources and licenses; the Linux archive is approximately 318 MiB and the Windows installer approximately 253 MiB.

You can open the local address printed in the terminal manually. The preferred port is `17321`; if occupied, the app chooses another available loopback port. Press `Ctrl+C` in the terminal to stop the application.

## Local data

On Linux, content and the database default to `~/.local/share/engelsiz`, configuration to `~/.config/engelsiz`, and cache to `~/.cache/engelsiz`. The `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, and `XDG_CACHE_HOME` overrides are supported. Removing the extracted program directory preserves this user data.

## Contribute

Bug reports, installation tests, documentation, translations, and code contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for local development and test commands. Reports can be written in Turkish or English.

- [Product scope and technical requirements, in Turkish](docs/main-spec.md)
- [Release validation and platform acceptance](docs/release-validation.md)
- [Packaging and third-party sources](third_party/README.md)
- [Open work and roadmap](https://github.com/yusuferdemyamali/engelsiz/issues)
- [Long-term vision, in Turkish](docs/vision.md): other open knowledge sources, education, and local AI are future directions, beyond the current alpha’s feature set.

## License and acknowledgements

Engelsiz’s own code is licensed under [MIT](LICENSE). Kiwix, its dependencies, and downloaded content retain their respective licenses. The distributed runtime’s corresponding sources and original notices are included under `LICENSES` in the package. See [third-party information](third_party/README.md).

Built with the work of [Kiwix](https://kiwix.org/), [openZIM](https://openzim.org/), and Wikipedia contributors.

> Keep knowledge with you before you need it.
