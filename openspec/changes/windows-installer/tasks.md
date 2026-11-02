## 1. Implementation

- [x] 1.1 Implement per-user x64 Inno Setup definition with stable identity, bundled runtime/notices and data-preserving uninstall; verify script assertions and compile on Windows CI.
- [ ] 1.2 Pin and safely stage Windows runtime with every required DLL; verify digest rejection and dependency smoke checks.

## 2. Distribution acceptance

- [x] 2.1 Supply reviewed complete runtime source/notices and approved project LICENSE; verify distributable staging passes compliance gate.
- [ ] 2.2 Run clean Windows 10/11 installation, browser startup, offline read and uninstall/reinstall preservation; record AC-01 evidence.

## Evidence (2026-11-02)

- 1.1: unchanged per-user Inno definition; native Windows fixture compile passed
  in run 41209502702. The real candidate was additionally compiled and
  installed/uninstalled/reinstalled with Inno 6.5.4 under isolated Wine.
  This closes the implementation/compiler task, not native user acceptance.
- 1.2: source-built 3.8.2 alternative has its own immutable runtime lock; the
  shipped runtime imports only reviewed Windows system DLLs. All 13 packaging
  tests and real ZIM/Wine dependency/startup smoke passed. Native Windows
  execution of this exact runtime remains pending, so the task stays open.
- 2.1: exact Windows final-link source/patch/CRT inventory, original notices and
  application inventory are bundled. `release.py` passed all existing gates and
  produced the real installer/checksum. No MSVC redistribution right was assumed.
- 2.2 remains open: no clean Windows 10/11 test; no native standard-user token,
  Start menu activation/default browser, disconnected restart or SmartScreen
  observation. Wine preservation results do not close this criterion.

See [candidate validation](../../../docs/windows-installer-validation.md) and
[artifact provenance](../../../third_party/runtime-source-candidates/windows-installer-verification.json).
