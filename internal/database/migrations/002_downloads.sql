CREATE TABLE downloads (
 id TEXT PRIMARY KEY,
 provider TEXT NOT NULL,
 provider_package_id TEXT NOT NULL,
 title TEXT NOT NULL,
 variant TEXT NOT NULL,
 file_name TEXT NOT NULL,
 source_url TEXT NOT NULL,
 temp_path TEXT NOT NULL,
 total_bytes INTEGER,
 downloaded_bytes INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('queued','downloading','paused','failed','cancelled','completed')),
 etag TEXT NOT NULL DEFAULT '',
 last_modified TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX downloads_unfinished ON downloads(provider, provider_package_id) WHERE status NOT IN ('cancelled','completed');
