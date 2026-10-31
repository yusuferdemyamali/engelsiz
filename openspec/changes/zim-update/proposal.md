## Why

Installed Wikipedia content can become outdated, but installing a newer file currently refreshes the entire Kiwix serving set without preserving a known usable selection. Users need to see a newer release and explicitly request an update while retaining their working offline Wikipedia if transfer, verification or activation fails.

## What Changes

- Compare installed releases with current catalog metadata for the same provider, content type, language and variant, and expose `update_available` with installed/new versions and download size.
- Add an explicit update action using the existing resumable download manager and existing verification/install controls; catalog checks never start downloads.
- Persist update associations and the committed active selection. Keep the old file serving during download and verification, and retain it after success.
- Activate a verified candidate with the existing single owned Kiwix process, check candidate usability and commit the new selection only on success. On failure restore the prior selection; interrupted attempts cannot become active at restart.
- Show progress, activation errors, retained older versions and retry/recovery actions without breaking offline search and reading.

## Capabilities

### New Capabilities

- `zim-update`: Explicit release comparison, safe update staging, durable activation and fallback, and user-visible update states.

### Modified Capabilities

None. Existing planning capabilities remain the baseline; this change adds update-specific selection and recovery rules.

## Impact

Go provider metadata comparison, package registry/migrations, update API, app activation coordination, Kiwix candidate usability checks and React package/download views. Existing generic transfer and package verification are reused. No new external dependency or production large-file fixture is required.

## Non-goals

Incremental patches, automatic downloads, binary self-update, torrents, mirror switching, two simultaneous Kiwix children and an old-version deletion interface. Verified older files remain retained; automatic cleanup is not required by the source specification.
