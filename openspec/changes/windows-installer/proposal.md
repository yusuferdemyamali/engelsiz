## Why

Windows users need one installer containing the app and offline runtime rather than developer setup steps.

## What Changes

- Build Engelsiz-Setup.exe with Inno Setup, per-user x64 Windows 10/11 installation, bundled runtime and notices, shortcuts and data-preserving uninstall.
- Fail closed when runtime hashes, licenses, notices or complete corresponding source are missing.
- Preserve user content and record unexecuted clean-machine acceptance checks as pending.

## Capabilities

### New Capabilities

- `windows-installer`: Verified windows installer delivery and validation.

### Modified Capabilities

None.

## Impact

Packaging scripts, platform installer/archive definitions, CI and release documentation. No auto-update, macOS, ARM or external publishing in this session.

## Distribution constraints

Kiwix tools are GPLv3-or-later ([official tagged README](https://github.com/kiwix/kiwix-tools/blob/3.8.2/README.md), [COPYING](https://github.com/kiwix/kiwix-tools/blob/3.8.2/COPYING)). Binary distribution requires corresponding source and license/notices, including incorporated dependency source and build scripts; a source URL for only kiwix-tools is insufficient. Packaging will require a reviewed, hash-pinned source/license bundle and an explicit project LICENSE. Their absence blocks distributable artifacts rather than fabricating compliance. See third_party/README.md.
