CREATE TABLE translation_models (
 id TEXT PRIMARY KEY,
 download_id TEXT UNIQUE,
 version TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK(status IN ('installing','ready','error')),
 size_bytes INTEGER,
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
