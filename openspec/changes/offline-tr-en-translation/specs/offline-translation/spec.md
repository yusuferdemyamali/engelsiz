## ADDED Requirements

### Requirement: Local bidirectional translation
The system SHALL use Argos Translate on the device for exactly tr→en and en→tr. Text SHALL NOT be sent to external services, logged or persistently stored.

#### Scenario: Translation with networking unavailable
- **GIVEN** the selected direction is installed
- **WHEN** the application and engine run with external networking unavailable
- **THEN** ordinary and multiline text including Turkish characters produces translated text in both supported directions.

### Requirement: Independent verified models
The system SHALL expose each direction's availability independently and download only on explicit user request using the existing Download Manager. Downloads SHALL be pinned to real versioned artifacts, expected byte sizes and SHA-256 digests. Installation SHALL be atomic and reject corrupt, unsafe or incompatible packages.

#### Scenario: One direction missing
- **GIVEN** only tr→en is installed
- **WHEN** tr→en is requested
- **THEN** translation works without downloading en→tr.

#### Scenario: Corrupt or interrupted model acquisition
- **WHEN** acquisition fails validation or is interrupted
- **THEN** a clear recoverable state is exposed, existing working directions remain usable, and no partial model is marked ready.

#### Scenario: Application restart
- **GIVEN** a model was installed successfully
- **WHEN** the application restarts
- **THEN** it detects and reuses the installed model without downloading again.

### Requirement: Safe bounded engine lifecycle
The backend SHALL validate language pairs, empty input and maximum input size, safely serialize or bound concurrent work, enforce timeouts and clean up child processes on shutdown. Engine failures SHALL not crash the Go application. A warm worker SHOULD reuse loaded models.

#### Scenario: Invalid request or unavailable engine
- **WHEN** an unsupported pair, invalid text, missing model or failed engine is encountered
- **THEN** the loopback API returns a bounded structured error without exposing text or internal paths.

### Requirement: Integrated translation workspace
The UI SHALL add Çeviri to existing navigation and use the current design system with design-taste-frontend. It SHALL support direction swap, multiline input, paste, clear, translation and copy, responsive layout and keyboard focus. Missing/downloading/installing/ready/translating/error states SHALL be distinguishable.

#### Scenario: Translation and copy
- **WHEN** the user enters text, translates and copies the result
- **THEN** the output is visible and copied to the clipboard without blocking navigation or silently starting downloads.

### Requirement: Self-contained distribution
Windows x86_64 and Linux distributions SHALL include the engine and required runtime dependencies but not translation models. Users SHALL NOT need Python, pip or terminal setup. Runtime and model paths SHALL support spaces and Unicode and respect existing application data conventions.

#### Scenario: Packaged application
- **GIVEN** a supported native platform package and no system Python
- **WHEN** installed models are used
- **THEN** the bundled engine runs using application-relative runtime paths and local data paths.
