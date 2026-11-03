# Offline-maps distribution candidates

Date: 2026-11-03. These are the exact source-built Kiwix runtimes shipped by
the `offline-maps` change, revalidated with current application notices.

Both runtimes are maintainer-selected **source builds**, not the default
official binaries pinned in `third_party/runtime-lock.json` (whose compliance
pins remain `null` and which this change does not alter):

- `linux-amd64/`: Kiwix Tools 3.8.2 built from the Ubuntu 22.04 baseline
  container. Runtime archive SHA-256
  `fd18dc8e60a1b4c70cda173db15cfea1ffcd1e2ea502aec2de2b4fa2401cbadb`.
  Source audit: `../linux-built-audit.json`.
- `windows-amd64/`: Kiwix Tools 3.8.2 cross-built for MinGW x86_64.
  Runtime archive SHA-256
  `b42623c579ed865fb175bddd8d1ad78e40319644ffe167e1f5e6b331f424bd9d`.
  Source/link audit: `../windows-built-audit.json`. This is unrelated to the
  official MSVC 3.8.1 binary and its redistribution requirements.

Each directory holds the candidate runtime lock (same runtime hash, fresh
manifest pin) and the reviewed compliance `manifest.json`. The manifests keep
all original component entries byte-identical and only refresh the
`applicationDependencies` pins to the current authenticated application
notices (Go `b7b70ab9…`, frontend `46683e6a…`, which include the new
MapLibre/PMTiles/Protomaps renderer dependencies). Review provenance is
recorded in the manifest fields (`originalManifestSha256`, `revalidatedBy`,
`revalidatedAt`); no human sign-off is invented and none is required by
`../README.md`.

Build the matching distributables with:

```sh
python scripts/release.py --platform linux-amd64 \
  --runtime-lock third_party/runtime-source-candidates/offline-maps/linux-amd64/candidate-runtime-lock.json \
  --runtime-archive dist/linux-source-candidate/kiwix-tools_linux-x86_64-3.8.2-known-source.tar.gz \
  --pmtiles-archive /path/to/go-pmtiles_1.31.2_Linux_x86_64.tar.gz \
  --compliance third_party/runtime-source-candidates/offline-maps/linux-amd64/manifest.json \
  --output /tmp/engelsiz-release --version 0.1.0
```

(Windows: same shape with the windows-amd64 lock, manifest, source-built zip
and Windows PMTiles archive.)
