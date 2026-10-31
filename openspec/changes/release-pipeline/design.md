## Context

See proposal.md and specs for the user contract. App runtime resolution already uses runtime/ adjacent to the actual executable. Frontend production files are embedded by internal/web. Existing upstream binary archives omit license/source material, and the project now uses the user-approved MIT license.

## Goals / Non-Goals

**Goals:** Repeatable staging, fail-closed runtime and compliance verification, user-data preservation and auditable acceptance status.

**Non-Goals:** Automatic publishing, unsigned trust bypass, AppImage/deb/rpm, binary auto-update, macOS or ARM.

## Decisions

- Use one standard-library Python release driver across build hosts; it verifies archive digests before safe extraction, only copies listed executables/dependencies, and validates each source/license inventory entry. Build prerequisites are for maintainers/CI only.
- The same driver accepts an explicitly selected source-audited local runtime lock for candidate packaging. The local Linux candidate is verified separately; configured native CI package jobs and desktop/Windows acceptance still require their own recorded execution. No candidate is silently substituted for the default official runtime pin.
- Pin Linux upstream Kiwix 3.8.2 static archive. ELF inspection found no PT_INTERP; the runtime reports libkiwix 14.2.0/libzim 9.5.0 and statically incorporated dependencies. Choose kernel 4.4 as a conservative product support baseline; [Go minimum requirements](https://go.dev/wiki/MinimumRequirements) document 3.2+ for Go 1.24 and later, so 4.4 is not a Go requirement. Require static app/runtime ELF at packaging, not a guessed glibc floor.
- Use published Windows Kiwix 3.8.1 x64 because the official 3.8.2 Windows URL returned 404. Include its entire verified runtime folder (including DLLs) and test its version on Windows CI. Use Inno Setup per-user x64 Windows 10/11 with constant AppId, program-only uninstall and explicit app-data preservation. A clean VM test remains mandatory.
- Require a local reviewed compliance bundle, hash-pinned in runtime-lock.json, including all corresponding sources/build scripts and license notices. Do not invent source completeness or change the approved Engelsiz MIT license. Package complete source material alongside notices for offline availability; CI refuses packaging until the bundle pin and project license are supplied.
- CI always runs platform tests and release-driver tests; opt-in workflow_dispatch packaging requires committed verified compliance metadata. Upload candidate assets with checksums, without a contents-write token or release publication.

## Risks / Trade-offs

- Missing source/license inventory → packaging remains blocked with an exact error; implementation tests use explicit synthetic fixtures rather than disguised release binaries.
- Cross-platform native dependencies → inspect Linux ELF and run runtime version on Windows; clean VM offline smoke remains required.
- Uninstaller could delete content → no UninstallDelete/data-directory sections; app data lives outside the install directory, covered by manual reinstall checks.
- Archive corruption/traversal → digest before extraction and reject unsafe/nonregular members.

## Migration Plan

No database changes. Maintainers supply reviewed compliance material and retain the approved MIT project license before packaging. Preserve all user data on install/uninstall. Publish only after docs/release-validation.md evidence has been filled and independently reviewed.
