# OpenSpec verification: Windows installer candidate

Verification date: 2026-11-02. Changes: `windows-installer`, `release-pipeline`.
Source commit of built application: `8f51fa844fabd1875e1cea3ea1d3aa743c5da2a6`.

| Dimension | Finding |
|---|---|
| Completeness | Windows 2/4 tasks; pipeline 2/4 tasks. Acceptance remains incomplete. |
| Correctness | Real installer/source/hash/PE gates and Wine integration passed. Clean native Windows scenarios unexecuted. |
| Coherence | Per-user Inno, adjacent runtime, embedded frontend, preserved app-data and fail-closed delivery retained. |

## Critical: required before release acceptance/archive

1. Windows task 1.2: execute the exact packaged runtime natively on supported
   Windows and retain runtime version/import/startup evidence. Static dependency
   inspection and Wine smoke are retained, but cannot substitute for this check.
2. Windows task 2.2: complete clean Windows 10 and 11 standard-user installation,
   Start menu/default browser, disconnected restart, Turkish paths and
   uninstall/reinstall preservation. Also record absence of developer tools and
   SmartScreen behavior. No native Windows version was tested in this session.
3. Pipeline task 2.1: run both actual native platform package jobs with available,
   reviewed compliance payloads. The local explicit Windows runtime lock passes;
   default official-runtime locks remain intentionally unapproved. Do not run a
   fixture through the distributable path or change the published Linux alpha.
4. Pipeline task 2.2: finish the AC/DoD matrix and independent release review.
   Existing matrix entries remain pending wherever not executed.

## Requirement/scenario evidence

- **Standalone Windows setup:** real `release.py` output includes app/runtime and
  LICENSES. `packaging/windows/engelsiz.iss` sets per-user installation and a
  stable AppId. Inno compilation, actual installed-file inspection, Unicode
  directory launch, search and article access passed under Wine. Clean install
  on native supported Windows remains pending.
- **Preserve user data:** no `[UninstallDelete]`; app-data is outside the program
  directory. The actual uninstaller preserved ZIM/database/partial/settings
  hashes; reinstall and subsequent search/article access passed, including in a
  loopback-only network namespace. Native scenario remains pending.
- **Verified lawful delivery:** `scripts/release.py` still verifies every runtime,
  manifest, source/license/notice and app-inventory pin. Twenty-one component
  records cover the new Windows final link and embedded assets. The official
  MSVC binary has not been falsely approved. Tests reject versioned compiler
  runtimes and missing arbitrary DLLs. Unversioned system `msvcrt.dll` has an
  explicit documented distinction. Missing corresponding-source tests pass.
- **Build matrix and traceability:** existing native Linux/Windows test matrix is
  retained. This candidate is a local explicit source build, not proof that both
  opt-in matrix package jobs ran. AC-01 and unexecuted manual criteria stay open.

No requirement was silently dropped or a test relaxed. The source-built 3.8.2
alternative is explicitly selected, as authorized by the maintainer. It requires
Unicode argv and machine-readable XML integer patches. Build inputs, original
notices, patches and the complete final link record accompany the artifact.

See [executed results and exact hashes](windows-installer-validation.md) and
[provenance](../third_party/runtime-source-candidates/windows-installer-verification.json).
All three verification dimensions were reviewed; no dimension was skipped.
**Four critical unfinished tasks remain across the two changes. Do not archive
or declare the Windows distribution release-ready.**
