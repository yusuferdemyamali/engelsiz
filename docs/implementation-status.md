# Implementation checkpoint

Updated: 2026-10-31. Working branch: `feat/8-zim-update`.

| Source change | OpenSpec change | State |
| --- | --- | --- |
| 01–07 | application-foundation through offline-wikipedia | Previously implemented and committed |
| 08 | zim-update | 8/8 tasks complete; independent review and real runtime success/rollback/offline scenarios passed |
| 09 | resilience-hardening | 4/4 tasks complete; diagnostics/privacy/log rotation review and aggregate checks passed |
| 10 | windows-installer | Installer definition and staging implemented; Wine compiler check passed; native CI, complete runtime/dependencies/source and clean Windows acceptance pending |
| 11 | linux-packaging | 3/4 tasks complete; source-audited Linux package produced and extracted-package/empty-rootfs tests passed; clean desktop/browser acceptance pending |
| 12 | release-pipeline | 2/4 tasks complete; workflow, safety tests and AC/DoD matrix verified; actual platform package jobs and release acceptance pending |

The user selected MIT; the project license is in `LICENSE`. Runtime and dependency licensing remains separate.

See [packaging review](packaging-review.md), [release validation](release-validation.md), [failure-mode matrix](failure-mode-matrix.md), and the verification/review files inside `openspec/changes/zim-update` and `openspec/changes/resilience-hardening`.

Current work is in the shared working tree. No new external release was published and completed changes have not been archived. Preserve unrelated existing files and changes.

Application Go/frontend attribution material and its collector are complete. Eleven packaging/attribution tests and actionlint pass; runtime bundles cannot reuse notices for stale app lockfiles.

The pinned Ubuntu Linux source build completed. Its byte-identical final-link audit, full source/notice payload, separate candidate runtime lock and package verification are recorded under `third_party/runtime-source-candidates/`. The candidate archive is `dist/linux-source-candidate/release-final/engelsiz-linux-x64.tar.gz` (SHA-256 `77b1678e50f56901b084765dccbd2ad618aeb8e0a8d2b5eca09efe7bcc7f9a7e`). The default official-runtime pins remain unchanged.

Next work: resolve Windows compiler-runtime deployment rights/materials or choose an alternative build; run native platform CI package jobs and record clean desktop installation/browser evidence. Keep acceptance tasks unchecked until their specified checks actually run.
