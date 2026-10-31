## 1. Implementation

- [ ] 1.1 Implement per-user x64 Inno Setup definition with stable identity, bundled runtime/notices and data-preserving uninstall; verify script assertions and compile on Windows CI.
- [ ] 1.2 Pin and safely stage Windows runtime with every required DLL; verify digest rejection and dependency smoke checks.

## 2. Distribution acceptance

- [ ] 2.1 Supply reviewed complete runtime source/notices and approved project LICENSE; verify distributable staging passes compliance gate.
- [ ] 2.2 Run clean Windows 10/11 installation, browser startup, offline read and uninstall/reinstall preservation; record AC-01 evidence.
