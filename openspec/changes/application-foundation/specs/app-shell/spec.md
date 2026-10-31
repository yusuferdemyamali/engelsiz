## Purpose

Provide a self-contained localhost application shell that starts reliably, stores metadata in OS-standard user directories, and shuts down without losing existing data.

## ADDED Requirements

### Requirement: Loopback-only HTTP server with port fallback

The system SHALL listen only on `127.0.0.1`, preferring port `17321` and falling back to a kernel-assigned loopback port when occupied.

#### Scenario: Preferred port free

- **WHEN** `127.0.0.1:17321` is free at startup
- **THEN** the server listens on `127.0.0.1:17321` and logs the address.

#### Scenario: Preferred port occupied

- **WHEN** `127.0.0.1:17321` is occupied
- **THEN** the app keeps running on another `127.0.0.1` port and uses that address for browser opening.

#### Scenario: No wildcard bind

- **WHEN** the server is running
- **THEN** no application listener is bound to `0.0.0.0` or `::`.

### Requirement: Health endpoint gates browser open

The system SHALL expose `GET /api/health` returning `200 {"status":"ok"}` and SHALL open the default browser with the actual server URL only after its readiness check succeeds.

#### Scenario: Cold start

- **WHEN** startup completes through self health-check
- **THEN** the default browser opens `http://127.0.0.1:<actual-port>/`.

#### Scenario: Backend not ready

- **WHEN** health-check has not succeeded or startup fails
- **THEN** no browser process is launched.

#### Scenario: No-browser flag

- **WHEN** the app is started with `--no-browser`
- **THEN** the server still listens and passes self health-check, but no browser process is launched.

### Requirement: Embedded frontend shell

The system SHALL serve its production frontend and static assets from the application binary without requiring a frontend development server, external assets, or developer runtimes.

#### Scenario: Single binary offline serve

- **WHEN** a built binary runs without the source/build directories and without internet access
- **THEN** `GET /` returns the embedded app shell with `200` and its referenced assets load from the same loopback origin.

#### Scenario: API routing remains separate

- **WHEN** the browser requests `GET /api/health`
- **THEN** the health JSON is returned rather than the frontend document.

### Requirement: OS-standard app directories

The system SHALL create config/data/cache dirs split by OS convention: on Linux config under `XDG_CONFIG_HOME`, DB/content/downloads/runtime/logs under `XDG_DATA_HOME`, and cache under `XDG_CACHE_HOME`; on Windows config/logs under `%APPDATA%/Engelsiz`, and DB/content/downloads/runtime/cache under `%LOCALAPPDATA%/Engelsiz`. The system SHALL never store application data beside the executable.

#### Scenario: First run

- **WHEN** the app starts with an empty profile
- **THEN** the DB, config, logs, downloads, content, runtime and cache locations are created under the appropriate user directories.

#### Scenario: Standard directory variables absent

- **WHEN** optional directory environment variables are unset
- **THEN** user-home-based OS-standard locations are used rather than the working directory.

### Requirement: SQLite with migrations, no silent reset

The system SHALL maintain SQLite metadata with versioned migrations creating `settings` and `providers` tables and SHALL fail visibly on corrupt DB or migration failure without resetting user data.

#### Scenario: Fresh install

- **WHEN** no database exists
- **THEN** all migrations apply exactly once and the app continues.

#### Scenario: Existing profile

- **WHEN** the application reopens an already migrated profile
- **THEN** completed migrations are not reapplied and existing metadata is preserved.

#### Scenario: Corrupt database or failed migration

- **WHEN** the database cannot be opened or migrated
- **THEN** startup aborts with an explicit logged error and no existing data file is deleted or reset.

### Requirement: Clean startup/shutdown

The system SHALL shut down gracefully on SIGINT, SIGTERM or application cancellation, closing the HTTP server and SQLite connection and releasing resources acquired during startup.

#### Scenario: Controlled shutdown

- **WHEN** SIGINT, SIGTERM or application cancellation is received while running
- **THEN** the server stops accepting connections, the DB closes, and the process exits successfully without an orphan listener.

#### Scenario: Cancellation during startup

- **WHEN** startup is cancelled before readiness completes
- **THEN** acquired server/DB resources are released and no browser is subsequently launched.

### Requirement: Basic file logging

The system SHALL record startup, shutdown, selected server address and DB/migration errors in the user log file, without recording unnecessary user content or large response bodies.

#### Scenario: Normal lifecycle

- **WHEN** the application starts and shuts down normally
- **THEN** its startup, actual loopback address and shutdown events are present in `logs/engelsiz.log` under the configured user directories.

#### Scenario: Logging unavailable

- **WHEN** the log file cannot be opened
- **THEN** the application reports that failure explicitly rather than silently claiming file logging is active.
