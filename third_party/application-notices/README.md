# Application attribution material

`go.txt` and `frontend.txt` retain original license, notice, patent-grant and copyright material for the locked application build. `inventory.json` records versions, source locations, source-file hashes, notice hashes, input lockfile hashes and final compendium hashes. These files cover Engelsiz's application dependencies; Kiwix and its incorporated dependencies belong to the separate runtime inventory.

Regenerate after `npm ci --prefix frontend`, using the exact release Go toolchain:

```sh
python scripts/collect-app-notices.py --go /path/to/go1.26.0/bin/go
python scripts/collect-app-notices.py --go /path/to/go1.26.0/bin/go --check
```

The collector reads the production `go list -deps` graph for Linux and Windows amd64 with CGO disabled and `GOAMD64=v1`, then runs `go mod verify` before reading dependency material. The inventory records the non-null `h1` module sums returned for all ten selected modules. It retains the union of selected modules, their root third-party notices, Go's LICENSE/PATENTS, the licenses/patent grants for stdlib-vendored x/* packages, and inline copyright comment blocks from selected Go/assembly files. This preserves additional attributions such as Sun's math notices and musl notices carried by modernc. Module-graph tools/test dependencies that do not occur in either production graph are not treated as shipped modules. The selected toolchain does not enable BoringCrypto; the Go boring package is its CGO-disabled stub, rather than a bundled BoringSSL binary.

For the frontend, all eight production packages in package-lock are retained as a conservative superset of the tree-shaken output. Tailwind CSS and its animation plugin supply generated CSS; Vite and esbuild are included conservatively for generated helper code. Other dev-only packages are recorded separately in the inventory. The full upstream Vite license includes its own bundled-dependency notices; their inclusion is not a claim that all Vite dependencies reach the browser. Every selected npm notice/entry-point attribution is compared with an HTTPS npm archive whose digest must match package-lock integrity. No files are extracted from the archive onto the filesystem.

The modernc upstream third-party notice files contain their own contextual sections for test-only or unlinked material. They are preserved in full, without changing their scope statements. Likewise, original module AUTHOR/CONTRIBUTOR documents and generic license appendices are retained as published.

When preparing a platform runtime inventory, copy these compendia into that bundle and set its `applicationDependencies.go.noticeFile`/`noticeSha256` and `applicationDependencies.frontend.noticeFile`/`noticeSha256` fields to the copied paths and inventory output hashes. A passing collector establishes the recorded notice-material inputs; it does not replace the runtime source/license or clean-machine release checks.

Normal CI and the release builder run the collector in `--check` mode after frontend dependencies are installed. The compliance gate additionally compares the inventory's four recorded app-input hashes with the project being packaged, validates the current compendia, and requires the bundled notice hashes to match those outputs. Updating app dependencies while retaining an old runtime manifest/notice bundle therefore blocks packaging until attribution is regenerated and the bundle is updated.
