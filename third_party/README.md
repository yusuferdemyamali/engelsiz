# Runtime distribution gate

Kiwix tools are GPL-3.0-or-later according to the [tagged upstream README](https://github.com/kiwix/kiwix-tools/blob/3.8.2/README.md). The [upstream GPL text](https://github.com/kiwix/kiwix-tools/blob/3.8.2/COPYING), especially sections 1 and 6, requires corresponding source when distributing object code. This includes incorporated dependencies and the build/install scripts needed to reproduce that binary; a link to the kiwix-tools repository alone is insufficient. The upstream binary archives inspected here contain tools only, without the complete source/license inventory. We do not assert that merely bundling a LICENSE satisfies redistribution.

The Linux 3.8.2 archive hash was computed from an existing official-download fixture; this is a content pin, not an upstream signature. Its runtime reports kiwix-tools 3.8.2, libkiwix 14.2.0, libzim 9.5.0, xapian 1.4.23, curl 8.4.0, microhttpd 0.9.76, zlib 1.3.1, ICU 73.2.0, pugixml 1.15.0, zstd 1.5.7 and lzma 5.2.6. This is an observed starting inventory, not proof that all linked components are covered. The upstream [kiwix-build project](https://github.com/kiwix/kiwix-build) provides build recipes and source archives; tie them to the exact binary's build before approval.

Windows uses the available official 3.8.1 archive; official 3.8.2 Windows URL returned 404 on 2026-10-30. The downloaded Windows archive SHA-256 is pinned in runtime-lock.json. It is flat and contains ICU 74 DLLs; actual PE inspection found missing MSVCP140.dll, VCRUNTIME140.dll and VCRUNTIME140_1.dll. Packaging rejects these missing compiler runtimes. Supply reviewed app-local redistribution files, their original license/terms and hashes as `runtimeExtraFiles` entries (`file`, `sha256`, `licenseFile`, `licenseSha256`) in the pinned compliance manifest; those files are copied next to kiwix-serve.exe and all shipped PE imports are checked. Delay-load imports fail closed until explicitly supported. [Microsoft redistribution terms](https://learn.microsoft.com/cpp/windows/redistributing-visual-cpp-files) and [dependency guidance](https://learn.microsoft.com/en-us/cpp/windows/determining-which-dlls-to-redistribute?view=msvc-170) require authorized redistributables; copying arbitrary DLLs from a developer machine is not a provenance solution. Clean Windows execution remains pending.

Before distributing either platform:

1. Keep the user-approved MIT project license at root LICENSE. The MIT app and separately executed GPL runtime retain their own license notices.
2. Obtain every exact runtime/dependency source archive, modifications/patches and build/install instructions, retain original copyright/license notices, and resolve any additional license obligations for all incorporated code.
3. Add `third_party/compliance/<platform>/manifest.json` and payload files. Review and pin the SHA-256 of that manifest as `complianceManifestSha256` in runtime-lock.json; runtimeSha256/version must match the binary pin. Manifest fields:

```json
{
  "runtimeSha256": "64 lowercase hex digits matching runtime-lock.json",
  "runtimeVersion": "exact pinned runtime version",
  "completeCorrespondingSource": true,
  "reviewedBy": "actual reviewer identity",
  "reviewedAt": "actual ISO date",
  "applicationDependencies": {
    "go": {"noticeFile": "licenses/go-dependencies.txt", "noticeSha256": "actual SHA-256"},
    "frontend": {"noticeFile": "licenses/frontend-dependencies.txt", "noticeSha256": "actual SHA-256"}
  },
  "components": [{
    "name": "kiwix-tools",
    "version": "exact source version",
    "license": "GPL-3.0-or-later",
    "licenseFile": "licenses/kiwix-tools-COPYING",
    "licenseSha256": "actual license file SHA-256",
    "sourceArchive": "sources/kiwix-tools.tar.xz",
    "sourceSha256": "actual source archive SHA-256",
    "buildInstructions": "build/README.md",
    "buildInstructionsSha256": "actual build instructions SHA-256"
  }]
}
```

Repeat components for every incorporated dependency, including statically linked code. `completeCorrespondingSource` records a documented completeness audit of the actual material, not an automated legal sufficiency claim or a separate user-approval requirement. The builder validates file presence/hash/path containment and preserves the entire reviewed bundle under LICENSES/runtime-corresponding-source. It cannot discover omitted dependencies or certify license compliance automatically.

4. Include notices/licenses for Engelsiz's Go/frontend dependencies as well in the reviewed distribution material; bundling this README alone is not sufficient.
5. Run the clean-machine matrix in docs/release-validation.md. Do not publish while its release gates are pending.

No binary/source payload is checked into this directory. Missing default pins/bundles deliberately stop official-runtime packaging before an artifact is produced.

A separately source-built Linux candidate is now available locally under `dist/linux-source-candidate/`, with its full source/notice bundle, its own immutable runtime/manifest hashes and extracted-package verification. Select it explicitly with `--runtime-lock dist/linux-source-candidate/candidate-runtime-lock.json` and its matching `--runtime-archive`; the default official-runtime pins remain unchanged. See [source-build audit](runtime-source-candidates/README.md). This candidate does not establish Windows redistribution rights or clean desktop release acceptance.
