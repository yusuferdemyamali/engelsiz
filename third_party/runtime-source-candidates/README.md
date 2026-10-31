# Runtime source candidates

Research date: 2026-10-31. These files record downloaded source material and upstream build recipes. They are **not** the `third_party/compliance/<platform>/manifest.json` accepted by the release builder, and do not set `completeCorrespondingSource` or authorize release publication.

## Linux 3.8.2

The official [Kiwix tools directory](https://download.kiwix.org/release/kiwix-tools/) provides the tools source archive separately from the binary. Its source archive contains the tools tree, not all statically incorporated dependencies.

The [r_100 build snapshot](https://github.com/kiwix/kiwix-build/tree/8062f23fb42454c9e6db319abe71d2add7263de7) records the 3.8.2 release recipe and patch tree. The [native_static / jammy release job](https://github.com/kiwix/kiwix-build/actions/runs/22567605021/job/65373400418) succeeded on 2026-03-02. The directory timestamp for the published Linux archive is 08:39 UTC, inside that job's 08:31–08:40 execution window. This identifies a concrete candidate build recipe; it is not a signed source-to-binary digest attestation.

Seventeen dependency source/patch downloads were checked against the SHA-256 values embedded in that recipe. The tools, libkiwix and libzim source archive hashes were also computed from official downloads. See linux-r100.json. Raw sources remain under /tmp/engelsiz-runtime-source-material; only metadata is checked into the project.

The top-level recipe's `versions.py` lists Xapian 1.4.26, while this Linux runtime reports 1.4.23. This is explained by the platform-specific [Xapian recipe](https://github.com/kiwix/kiwix-build/blob/8062f23fb42454c9e6db319abe71d2add7263de7/kiwixbuild/dependencies/xapian.py): Linux uses a hash-pinned 1.4.23 archive; Windows uses the Git source. This illustrates why a top-level version list alone is insufficient evidence.

Still required: review transitive incorporated-code and license coverage, retain every applicable original notice and patch, and record complete build instructions and application dependency notices. A separate isolated build from captured inputs can produce a runtime whose source closure is directly known, without claiming bit-for-bit reproduction of the upstream executable.

The first isolated native attempt under /tmp/engelsiz-known-source-build was stopped before producing a distributable runtime. Its host static C/C++ libraries belong to CachyOS x86_64_v4 packages; the host glibc ABI note specifies Linux 6.1.0. Those inputs cannot establish the product's generic x86_64 / kernel 4.4 baseline. No host package was installed and no runtime pin changed.

A fresh attempt uses a rootless disposable Ubuntu 22.04 container, pinned by digest in build-linux-container.sh, and /tmp/engelsiz-baseline-source-build for all outputs. Vendor objects are rebuilt with `-O2 -march=x86-64 -mtune=generic`, Meson 1.9.2 and four compile jobs. The container's glibc ABI note specifies Linux 3.2.0. Container-only development packages, their source versions, static archive hashes/owners, original notices and downloaded glibc/GCC source archives are recorded under provenance/. The driver refuses accidental host execution and does not update release pins. Live output is container-driver.log; step logs remain under LOGS and BUILD_native_static/LOGS. Completion, actual link closure and any required local patches still need review before using the output.

The container image and vendor source recipe are immutable pins. Ubuntu package repositories and transitive Python package resolutions are captured by actual versions/hashes during the attempt; the current research driver does not promise a bit-reproducible future rebuild from mutable repositories.

### Completed local source build

The Ubuntu baseline build completed, including the upstream build/test/install steps. The resulting `kiwix-serve` SHA-256 is `d90ee80c33685187f4fbb4e57284e4ad81666fe374a981bfd1c7fdf1d1541cae`. Its ELF notes require x86-64-baseline and report Linux ABI 3.2. The final link was repeated with a link map and archive trace; its unstripped output matched the original build, and the same install strip reproduced the installed binary byte-for-byte. All linked vendor archives and host C/C++/CRT inputs are recorded. See [linux-built-audit.json](linux-built-audit.json).

The source-audited **local build** has its own runtime lock and compliance manifest under `dist/linux-source-candidate/`. It does not certify reproduction of the official executable or replace `third_party/runtime-lock.json`. The full preferred-source snapshot, original archives, patches, host-runtime source packages/notices, build instructions, application attribution and embedded-font/JavaScript notices are included in its package. Individual font/JavaScript license payloads are separately hash-bound in that manifest.

Package: `dist/linux-source-candidate/release-final/engelsiz-linux-x64.tar.gz`, SHA-256 `77b1678e50f56901b084765dccbd2ad618aeb8e0a8d2b5eca09efe7bcc7f9a7e`. Verification of the extracted package and its empty-rootfs offline smoke is recorded in [linux-package-verification.json](linux-package-verification.json). This does not close clean desktop/browser or Windows acceptance.

## Windows 3.8.1

The official ZIP timestamp is 2025-12-02 06:17 UTC; the [r_91 recipe](https://github.com/kiwix/kiwix-build/tree/6f3a27d3988c424b5dbb82a29ff508aad0055211) records the corresponding 3.8.1 release at 06:03 UTC with libkiwix 14.1.1 and libzim 9.4.0. Their official source archives and the recipe snapshot were downloaded and hashed; see windows-r91.json.

The Linux source set does not establish Windows coverage. Review Windows-specific Xapian/Boost.Regex refs, ICU 74.1 sources/notices and the original Visual C++ redistribution files and terms before building a distribution bundle. The existing dependency gate continues to reject missing compiler DLLs.
