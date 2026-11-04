# Pinned Kiwix runtime archives without a public download URL

The default `third_party/runtime-lock.json` Linux entry points at the
official Kiwix download, which CI fetches with `--download-runtime`.
The Windows entry is a **source-built** MinGW 3.8.2 runtime
(`b42623c5…`) with no public URL, so the exact reviewed archive is
committed here. `scripts/release.py` hash-checks it against the lock
(`checked()`); a single changed byte fails the build.

- `kiwix-tools_windows-x86_64-3.8.2-known-source.zip`
  SHA-256: `b42623c579ed865fb175bddd8d1ad78e40319644ffe167e1f5e6b331f424bd9d`
  (16.4 MB)
  Built from the audited source/patch payload; see
  `third_party/runtime-source-candidates/` (especially
  `windows-built-audit.json`, `windows-built-runtime-lock.json`,
  `windows-installer-verification.json`) and
  `docs/windows-installer-validation.md`.
  Compliance manifest: `ed93ec51300f43bc1de57320ca10255b04e7679a46d46610abd228aa8bda9707`
  (materialized in CI from v0.1.0-alpha.3 assets; never committed).

Do not replace this file without re-review and a lock/manifest update.
The 228 MB corresponding-source archive is intentionally NOT committed;
CI materializes the compliance bundle (including sources) from the
authenticated v0.1.0-alpha.3 release assets.
