# v0.1 release validation

Status: NOT RELEASE-READY. A successful cross-build/unit test is not evidence of a clean-machine installation. Keep each unexecuted check pending; record OS image/version, app commit, package SHA-256, date, tester and artifact/log links when completed. No external GitHub Release is published by the workflow.

## Build and packaging

Maintainer prerequisites: Go 1.26.0, Node 22/npm, Python 3.12+, and Inno Setup 6 on Windows. These tools are NOT end-user dependencies.

The Windows CI job compiles the installer definition on every run using `scripts/check-installer.py` and the freshly built application. Its temporary runtime is a compiler fixture, not Kiwix; the resulting setup is deleted and never uploaded. This checks Inno syntax without bypassing the real runtime/source/dependency gates. It does not establish clean-machine acceptance.

```
python -m unittest discover -s scripts/tests -v
python scripts/release.py --platform linux-amd64 --runtime-archive /path/to/pinned.tar.gz --compliance third_party/compliance/linux-amd64 --output /tmp/engelsiz-release
python scripts/release.py --platform windows-amd64 --runtime-archive C:\path\to\pinned.zip --compliance third_party/compliance/windows-amd64 --output C:\temp\engelsiz-release
```

Or run the GitHub Actions workflow manually with package=true after committing reviewed compliance manifests/payloads. Digest pins precede extraction; missing LICENSE/source/notices block packaging. Packaging rebuilds and embeds the frontend and makes a static Go build. Only an empty output directory is accepted. Generated packages have companion `.sha256` files. Source payloads/notices are included under LICENSES; see [runtime distribution gate](../third_party/README.md).

The source-built local Linux candidate can be reproduced from its supplied immutable inputs:

```sh
python scripts/release.py --platform linux-amd64 \
  --runtime-lock dist/linux-source-candidate/candidate-runtime-lock.json \
  --runtime-archive dist/linux-source-candidate/kiwix-tools_linux-x86_64-3.8.2-known-source.tar.gz \
  --compliance dist/linux-source-candidate/compliance --output /tmp/engelsiz-candidate-repack
```

That command uses the audited local source build rather than the default official download. Its source/material audit and extracted-package headless tests passed; clean desktop/browser acceptance and native Windows CI remain pending. See [packaging review](packaging-review.md).

Supported targets: Windows 10/11 x86_64 and Linux x86_64 kernel 4.4+ desktop with browser/xdg-open. Kernel4.4 is a conservative Engelsiz product baseline, not a claim about [Go's minimum kernel requirement](https://go.dev/wiki/MinimumRequirements). Linux app/runtime packaging requires static ELF without PT_INTERP/PT_DYNAMIC; no particular installed glibc version is required by those binaries. Browser/opener/desktop availability remains a host prerequisite. Windows CI version/dependency smoke cannot replace clean VM acceptance.

## Clean-machine procedures

Windows: use separate clean Windows 10 and11 x64 VM snapshots with no Go, Node, SQLite, Kiwix or developer PATH. Verify package hash, run setup as a standard user, start via Start menu, confirm browser health and loopback binding, install a test ZIM, close cleanly, reopen with network disconnected, search/read, uninstall, verify app-data content/hash unchanged, reinstall and search/read again. Test installation path containing spaces and non-ASCII username. Record SmartScreen/signing behavior; no signing certificate is provisioned here.

Linux: use clean supported distro snapshots with no Go/Node/Kiwix, verify archive hash, extract into a path with spaces, launch by absolute path from a different cwd, check browser/loopback health, install a test ZIM, close/reopen offline, search/read and inspect XDG-only writes. Test both default and overridden XDG paths. Delete only the extracted program folder and verify user data remains. Record OS/kernel/browser/architecture and absence of developer dependencies.

For both targets use a local tiny real ZIM fixture for ordinary integration. Test live Turkish catalog separately. A production Wikipedia transfer/resume is a manual acceptance check and must be explicitly recorded, not inferred from a small fixture.

## Acceptance criteria traceability

Every row requires evidence against the exact release candidate. Automated references identify relevant coverage, not an assertion that a future package has passed.

| Criterion | Evidence/check | Candidate status |
|---|---|---|
| AC-01 Windows install | Clean Windows 10/11 procedure above; bundled runtime version/DLL startup; standard-user setup and reinstall | PENDING |
| AC-02 Linux install | Clean Linux procedure above; static ELF, cwd-independent runtime and no developer installs | Headless extracted-package and empty-rootfs passed; PENDING clean desktop/browser |
| AC-03 Localhost safety | `TestListenLoopbackOnly`, runtime listener tests; VM LAN peer cannot connect to bound ports | PENDING package smoke |
| AC-04 Browser startup | `waitForHealth` app tests; observe default browser opens actual selected port after readiness | PENDING manual browser |
| AC-05 Live catalog | provider catalog/acquisition tests; refresh live catalog and record actual upstream identity | PENDING live catalog |
| AC-06 Variants | catalog tests; observe Mini/Nopic/Maxi upstream versions/size or explicit absent variant | PENDING live UI |
| AC-07 Download/resume | download manager interruption/Range tests; pause/restart/resume actual Wikipedia download preserving bytes | PENDING large-file manual |
| AC-08 Disk capacity | download space-check tests; insufficient-space response and preserved partial/file hashes | PENDING package smoke |
| AC-09 Verification | packages hash/size/fault tests; corrupt candidate and confirm rejected installed state | PENDING package smoke |
| AC-10 Offline use | update/app/search tests and tiny real ZIM smoke; cold offline package restart/search/read | PENDING clean-machine |
| AC-11 Catalog offline | catalog/API failure tests; disconnect network, retry catalog while old content remains readable | PENDING package smoke |
| AC-12 Update detection | update comparison/UI tests; see newer version/size without POST until explicit Güncelle | PENDING package smoke |
| AC-13 Safe update | activation/recovery tests; valid update, hash failure, invalid ZIM and cold rollback with old file hash unchanged | PENDING package smoke |
| AC-14 Child cleanup | runtime lifecycle/stop tests; close app and inspect no owned kiwix child remains | PENDING platform smoke |
| AC-15 Port collision | `TestListenFallbackWhenPreferredOccupied`; occupy17321 then start package/browser on fallback | PENDING platform smoke |
| AC-16 Provider boundary | API/search/provider tests and frontend review; browser network uses same app origin only | PENDING package inspection |
| AC-17 Generic manager | download manager/provider acquisition tests; transfer layer contains no Wikipedia-specific decisions | PENDING final review |
| AC-18 Scope | inspect package/routes and planned changes; no AI/maps/mobile/plugin placeholders | PENDING final review |

## Definition of Done traceability

| DoD item | Acceptance mapping | Candidate status |
|---|---|---|
| 1 Windows release installable | AC-01 | PENDING |
| 2 Linux standalone release | AC-02 | PENDING |
| 3 Browser localhost UI | AC-03,04 | PENDING |
| 4 Discover Turkish Wikipedia | AC-05 | PENDING |
| 5 Manage Mini/Nopic/Maxi | AC-06 | PENDING |
| 6 Safely download ZIM | AC-07,08,17 | PENDING |
| 7 Resume interrupted download | AC-07 | PENDING |
| 8 Verify download | AC-09 | PENDING |
| 9 Serve ZIM via Kiwix | AC-10,16 | PENDING |
| 10 Search | AC-10 | PENDING |
| 11 Read article | AC-10 | PENDING |
| 12 Offline9–11 | AC-10,11 | PENDING |
| 13 Detect updates | AC-12 | PENDING |
| 14 Preserve old version | AC-13 | PENDING |
| 15 Child process cleanup | AC-14 | PENDING |
| 16 Tests/manual evidence | All AC rows and retained evidence | PENDING |

## Release gates

- [ ] Hash-pinned reviewed complete runtime source/build/license inventory for each platform, including incorporated dependencies and app Go/frontend notices.
- [ ] Windows archive SHA-256 pin and actual runtime/DLL smoke verified.
- [ ] Inno Setup compile and setup/uninstall/reinstall executed on clean Windows 10/11.
- [ ] Linux tarball smoke executed on clean supported environment.
- [ ] Both matrix CI jobs produce reviewed packages and matching checksums.
- [ ] All manual/automated rows filled with actual candidate evidence and independently reviewed.

Until all gates pass, artifacts are engineering candidates and v0.1 acceptance remains incomplete.
