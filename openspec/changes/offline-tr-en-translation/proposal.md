## Why

Engelsiz users need Turkish–English translation without internet, accounts or external services. This explicitly requested change extends the current product scope; it does not add general local AI or a third language.

## What Changes

- Add an Argos Translate module for Turkish → English and English → Turkish.
- Download each model only on explicit user action through the existing provider-independent Download Manager; independently verify and install each direction.
- Bundle the translation engine and its Python dependencies in Windows x86_64 and Linux releases. Models remain separate downloads.
- Add a theme-consistent, keyboard-accessible Çeviri page using the existing navigation and components. All frontend work uses design-taste-frontend.
- Keep translation text local and ephemeral. Preserve Wikipedia, maps and loopback-only security.

## Capabilities

### New Capabilities

- `offline-translation`: explicit model acquisition, independent directional readiness, local translation and process lifecycle.

### Modified Capabilities

- Application shell navigation and release packaging gain the translation module.

## Impact

Go provider/service/API wiring, a persistent Argos worker, native-platform runtime packaging, React UI and targeted tests. No language detection, speech, OCR, history, accounts, clouds or unrelated refactoring.
