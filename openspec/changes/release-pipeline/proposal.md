## Why

Both platforms need repeatable build/test/package output and honest evidence for the v0.1 acceptance contract.

## What Changes

- Add GitHub Actions platform matrix, frontend embed, Go tests, verified runtime staging, packaging, checksums and release-ready artifacts plus a traced manual validation checklist.
- Fail closed when runtime hashes, licenses, notices or complete corresponding source are missing.
- Preserve user content and record unexecuted clean-machine acceptance checks as pending.

## Capabilities

### New Capabilities

- `release-pipeline`: Verified release pipeline delivery and validation.

### Modified Capabilities

None.

## Impact

Packaging scripts, platform installer/archive definitions, CI and release documentation. No auto-update, macOS, ARM or external publishing in this session.

## Distribution constraints

Kiwix tools are GPLv3-or-later ([official tagged README](https://github.com/kiwix/kiwix-tools/blob/3.8.2/README.md), [COPYING](https://github.com/kiwix/kiwix-tools/blob/3.8.2/COPYING)). Binary distribution requires corresponding source and license/notices, including incorporated dependency source and build scripts; a source URL for only kiwix-tools is insufficient. Packaging will require a reviewed, hash-pinned source/license bundle and an explicit project LICENSE. Their absence blocks distributable artifacts rather than fabricating compliance. See third_party/README.md.
