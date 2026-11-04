# Pinned Kiwix runtime archives without a public download URL

Both platform runtimes are **source-built** 3.8.2 with no public URL, so
the exact reviewed archives are committed here. `scripts/release.py`
hash-checks them against the candidate locks (`checked()`); a single
changed byte fails the build.

- `kiwix-tools_linux-x86_64-3.8.2-known-source.tar.gz`
  SHA-256: `fd18dc8e60a1b4c70cda173db15cfea1ffcd1e2ea502aec2de2b4fa2401cbadb`
  (7.1 MB)
  Lock: `third_party/runtime-source-candidates/offline-maps/linux-amd64/candidate-runtime-lock.json`
  Compliance manifest: `2e8353cd2b8920178d11c1dc4b60b112b9c3aed8e70711b54269b66074dfe5f9`
  (materialized in CI from v0.1.0-alpha.3 assets; never committed).
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
