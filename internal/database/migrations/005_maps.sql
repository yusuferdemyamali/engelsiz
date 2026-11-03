CREATE TABLE map_packages (
 id TEXT PRIMARY KEY,
 title TEXT NOT NULL,
 version TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','preparing','downloading','verifying','ready','error')),
 size_bytes INTEGER,
 error TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
