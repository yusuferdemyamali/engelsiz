# PMTiles executable attribution

`notices.txt` preserves original license, patent, notice and copyright material for the modules and source files selected in the official PMTiles 1.31.2 Linux and Windows x64 executables. `inventory.json` records each source, content hash, Go module checksum, selected package and both release archive hashes. The upstream executable's own `LICENSE` is also kept next to it in the packaged runtime.

The collector checks both pinned archive hashes, the binaries' embedded Go module graphs and upstream source revision against the exact Go 1.26.1 source build graph, verifies the downloaded Go modules, and collects original root notices and inline copyright blocks. It includes Go standard library and selected vendored-module notices. A matching graph is not a claim that the official binaries were independently reproduced, nor a substitute for any separate legal or clean-machine distribution review.

Regenerate or verify using official pinned release archives and Go 1.26.1:

```
python scripts/collect-pmtiles-notices.py --go /path/to/go1.26.1/bin/go --linux-archive /path/to/Linux_x86_64.tar.gz --windows-archive /path/to/Windows_x86_64.zip
python scripts/collect-pmtiles-notices.py --go /path/to/go1.26.1/bin/go --linux-archive /path/to/Linux_x86_64.tar.gz --windows-archive /path/to/Windows_x86_64.zip --check
```

After changes, pin the SHA-256 of `notices.txt` and `inventory.json` in both platform records of `third_party/pmtiles-lock.json`. The release builder validates these files against the pins and packages this directory. The separately reviewed Kiwix runtime corresponding-source bundle remains required.
