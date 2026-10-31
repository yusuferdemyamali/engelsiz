# Packaging and pipeline review

Updated: 2026-10-31. Changes: windows-installer, linux-packaging, release-pipeline.

## Implemented and checked

- The release driver verifies runtime hashes before extraction, rejects traversal, links and duplicate names, and stages runtime files with their required dependencies.
- A pinned compliance manifest binds source archives, license texts, build instructions and application dependency notices to their hashes. Application notice inventories must match the current lock/config files and local compendia; stale bundled notices are rejected. CI and packaging also authenticate notices with the reproducible collector. This is an inventory check, not an automatic claim that omitted dependencies are covered.
- Linux packaging rejects dynamic ELF dependencies and normalizes tar order, owner, timestamps, modes and gzip headers. Tests verify identical archives even when input permissions differ.
- Windows import inspection rejects missing non-system DLLs and unsupported delay-load imports. Native x64 installer selection excludes ARM; per-user installation uses a stable application identity and does not delete app-data on uninstall.
- GitHub Actions tests both platforms, builds the embedded frontend/application and provides opt-in package/checksum artifacts. The workflow has read-only repository permissions and does not publish a GitHub Release.
- The Windows CI job also compiles the actual installer definition with a temporary non-release fixture, independently of the source-bundle gate. Fixture installers are deleted and never uploaded.

Earlier independent review identified predictable temporary-download paths, archive mode nondeterminism, unpinned license/build-instruction payloads and incomplete Windows import inspection. These were fixed. The final delay-load rejection has a regression test.

## Executed validation

- Eleven Python packaging/attribution tests passed on 2026-10-31, covering hash/path rejection, Windows DLL retention and missing/delay-loaded dependencies, compliance and stale-notice tampering, exact notice payload authentication, static ELF checks, archive determinism and acceptance-matrix completeness.
- `actionlint` 1.7.7 accepted `.github/workflows/release.yml`.
- All twelve OpenSpec changes passed strict validation. This validates artifacts, not release acceptance.
- The current Windows application cross-compiled successfully. The installer definition then compiled successfully with official Inno Setup 6.7.4 under an isolated Wine prefix, including staging/output paths with spaces. The compiler download matched its official GitHub release asset digest (`a549929319f9120716677c685535ca4671e4b937b5fdc87e2585f32d8eb30e8c`). This was a compiler fixture, not a distributable runtime package or a Windows CI/VM acceptance run.
- The current Linux application and pinned real Kiwix runtime passed static x86_64 ELF checks. An assembled bundle launched from an unrelated directory with spaces, discovered its adjacent runtime without an override, loaded a small verified registry fixture, served the embedded frontend, searched/read real ZIM content and shut down cleanly. Local evidence: `/tmp/engelsiz-linux-bundle-6u54ij0g/evidence.json`. This was not a clean-machine run of a final compliant tarball.
- The official Windows archive was downloaded again and matched its pinned SHA-256. Its PE import inspection reports three missing app-local libraries: `msvcp140.dll`, `vcruntime140.dll` and `vcruntime140_1.dll`. Local inventory: `/tmp/engelsiz-windows-dependencies-j3lmifg3/inspection.json`.
- `git diff --check` and Python syntax checks passed.
- Application Go/frontend attribution compendia and their reproducible version/hash inventory are in [application-notices](../third_party/application-notices/README.md). The collector authenticates Go module directories with `go mod verify`, npm payloads against exact lockfile archive integrity, and rejects stale generated output. Release builds, the collector and CI explicitly use `GOAMD64=v1`.

Runtime source candidates, recipe provenance and baseline-container build scripts are in [runtime-source-candidates](../third_party/runtime-source-candidates/README.md). A native host build was stopped after discovering that its CachyOS system libraries require x86-64-v4 and Linux 6.1. It is not a release candidate. The replacement build uses a pinned Ubuntu 22.04 image, fresh compiler objects, generic x64 flags, and captured original host-library source/patches/notices. Its verified local output remains separate from the default official runtime pin.

## Completed source-built Linux candidate

The baseline build and final source/link audit completed. All vendor archives used by the final link are covered by the source snapshot; Ubuntu glibc/GCC original sources and Debian patches cover the host static C/C++/CRT inputs. A repeated link reproduced the unstripped binary, and GNU strip reproduced the installed runtime hash exactly. Original vendor/host notices, full application attribution, unmodified browser asset sources and original font/JavaScript license texts are included. The local candidate manifest separately binds the font/JavaScript license payloads by hash. This is a documented material audit of this known-source build, not a claim to reproduce the official executable or an automatic legal certificate.

The release builder successfully accepted that separate immutable runtime lock and source/notice bundle and produced `dist/linux-source-candidate/release-final/engelsiz-linux-x64.tar.gz` (308,240,346 bytes, SHA-256 `77b1678e50f56901b084765dccbd2ad618aeb8e0a8d2b5eca09efe7bcc7f9a7e`). Its large size includes the complete source/notice payload. Default official runtime pins were retained.

The exact final archive checksum and layout were verified, then its extracted app/runtime passed static checks, cwd-independent discovery, real-ZIM search/read, embedded frontend and clean shutdown. The same extracted binaries also ran successfully in an empty container rootfs with no Go, Node, SQLite CLI, external Kiwix installation or shared-library installation. ELF notes identify generic x64 and Linux ABI 3.2. These headless checks do not substitute for the documented clean desktop/browser procedure. Durable evidence: [source audit](../third_party/runtime-source-candidates/linux-built-audit.json), [package verification](../third_party/runtime-source-candidates/linux-package-verification.json).

## Remaining release evidence

1. The local source-built Linux candidate's source/notice audit is complete. Default official-runtime CI packaging still requires its configured corresponding-source bundle; the local candidate does not assert source-to-binary equivalence for the upstream download. Windows-specific source and notice closure remains separate work.
2. The Windows runtime still needs its app-local compiler libraries and documented redistribution rights. Microsoft's [redistribution documentation](https://learn.microsoft.com/en-us/cpp/windows/redistributing-visual-cpp-files?view=msvc-170) limits redistribution to appropriately licensed Visual Studio users. The user's own MIT project license does not establish those separate rights.
3. Execute the native Windows CI installer check and actual package jobs, then the clean Windows/Linux procedures in [release-validation.md](release-validation.md). Wine compilation and assembled local bundles do not satisfy AC-01/AC-02 by themselves.

The Linux archive is a verified candidate with desktop acceptance pending. No v0.1 release-readiness claim or Windows distribution is made by this report.
