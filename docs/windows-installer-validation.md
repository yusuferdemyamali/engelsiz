# Windows source-built installer candidate

Status: **NOT RELEASE-READY**. This work addresses #10 and the Windows part of
#12. A real source-built runtime candidate is being packaged; Wine tests do not
establish clean Windows acceptance. No Windows version has been verified on a
clean native machine in this session. The existing Linux alpha release is unchanged.

## Runtime choice and source review

The official Kiwix Tools 3.8.1 ZIP matches its pinned SHA-256. Its eight PE files
were inspected, and MSVCP140.dll, VCRUNTIME140.dll and VCRUNTIME140_1.dll are
missing. All five ICU DLLs match the Unicode ICU 74.1 Windows release byte for
byte. Windows-specific Xapian and Boost.Regex source candidates and ICU sources
were obtained from upstream, rather than inferred from the Linux inventory.
See [official-binary investigation](../third_party/runtime-source-candidates/windows-r91.json).

Microsoft restricts MSVC redistributable distribution to licensed Visual Studio
users under its [redistribution terms](https://learn.microsoft.com/cpp/windows/redistributing-visual-cpp-files).
The maintainer selected an alternative source build; no MSVC redistribution
entitlement was assumed and no Microsoft redistributable was copied.

The alternative is Kiwix Tools **3.8.2**, libkiwix 14.2.0, libzim 9.5.0,
Xapian 1.4.23, ICU 73.2, curl 8.4.0, microhttpd 0.9.76, zlib 1.3.1,
pugixml 1.15, zstd 1.5.7 and LZMA 5.2.6. Every vendor archive in the final
Windows link was rebuilt for MinGW x86_64. The retained source snapshot is an
input to that new build; the earlier Linux audit is not its certification.

Two local patches are required:

- Unicode `wmain` arguments converted to UTF-8, so Turkish Windows paths survive
  command-line parsing. The unpatched runtime failed the real ZIM path test.
- Ungrouped XML/OpenSearch integers. Upstream returned `2,414`, which the app's
  strict numeric parser correctly rejected. The Windows build now emits `2414`;
  HTML formatting is retained.

The build also installs the actual cross-compiled ICU static data archive in
place of the stub. GCC C++/exception support, the MinGW CRT and winpthreads are
static; original terms, the GCC Runtime Library Exception, exact GCC Built-Using
sources and MinGW source/patch packages are retained. Unversioned `msvcrt.dll`
is the [Windows system CRT](https://devblogs.microsoft.com/oldnewthing/20140411-00/?p=1273),
distinct from versioned compiler redistributables. Packaging tests continue to
reject MSVCR100, MSVCP140, VCRUNTIME140 and unbundled MinGW DLLs.

## Rebuild and inspect

[The container build driver](../third_party/runtime-source-candidates/build-windows-container-inner.sh)
and [patches](../third_party/runtime-source-candidates/windows-patches/) are
retained with the source payload. `compliance/build/BUILD.md` records environment,
input layout and commands. The source/patch/notice payload, final linker command,
map, trace and hashes of every selected archive/CRT input are kept in the local
candidate's `compliance/` directory and are bundled under `LICENSES`.

The final link was repeated with timestamps disabled and compared before
stripping. This proves consistency of the captured link inputs, not a
bit-reproducible future full build from mutable package repositories.

The [Windows source/link audit](../third_party/runtime-source-candidates/windows-built-audit.json)
and [candidate runtime lock](../third_party/runtime-source-candidates/windows-built-runtime-lock.json)
record the captured hashes and results. The default official runtime lock is unchanged. Select the local Windows
candidate explicitly:

```sh
python scripts/release.py --platform windows-amd64 \
  --runtime-lock dist/windows-research/candidate/candidate-runtime-lock.json \
  --runtime-archive dist/windows-research/candidate/kiwix-tools_windows-x86_64-3.8.2-known-source.zip \
  --compliance dist/windows-research/candidate/compliance \
  --output dist/windows-research/candidate/release --version 0.1.0-alpha.2
```

Packaging uses Go 1.26.0, Node 22, Python 3.12+ and Inno Setup 6.5.4. On this
Linux host, an isolated Wine prefix runs the actual Inno compiler via a path
adapter named `iscc`. The release builder's compliance/hash/dependency checks
remain active. This does not replace native installer acceptance.

The real-runtime regression tool runs natively on Windows or explicitly under
Wine. Supply an indexed ZIM and a query with results:

```powershell
python scripts/check-windows-runtime.py --runtime C:\candidate\runtime\kiwix-serve.exe --zim C:\fixtures\wikipedia_en_climate_change_mini_2024-06.zim --query climate --output C:\evidence\runtime.json
```

Python is a maintainer QA prerequisite for this script, not an application
prerequisite. End-user validation must separately use an environment without
developer tools. The script tests Turkish/space paths, two cold starts, numeric
RSS results and actual article bytes. It never labels its result clean-machine
acceptance or claims to have disconnected the network.

## Required native acceptance

Use clean Windows 10 and 11 x64 snapshots, a standard account, and the exact
installer/checksum. Record OS build, browser, tester, artifact hash and source
commit. The maintainer has a dual-boot Windows 11 installation; it is inactive
while this Linux session runs and has not been used as a VM or modified.

Follow [the complete procedure](release-validation.md#clean-machine-procedures):
per-user setup without elevation, Start menu/default browser, localhost-only
listeners, real ZIM acquisition, search/article rendering, disconnected cold
restart, Turkish username and space paths, uninstall/reinstall preserving ZIMs,
partial transfers, SQLite and settings. Capture SmartScreen/signature behavior.
A dual-boot developer installation, Wine, a hosted CI runner or compiler fixture
cannot close the clean-machine criteria. All unexecuted acceptance remains pending.
