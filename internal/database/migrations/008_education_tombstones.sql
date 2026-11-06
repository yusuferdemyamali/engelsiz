CREATE TABLE education_tombstones (
 download_id TEXT PRIMARY KEY,
 resource_id TEXT NOT NULL,
 created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
