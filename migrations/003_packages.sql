ALTER TABLE downloads ADD COLUMN content_type TEXT;
ALTER TABLE downloads ADD COLUMN language TEXT;
ALTER TABLE downloads ADD COLUMN version TEXT;
ALTER TABLE downloads ADD COLUMN expected_size INTEGER;
ALTER TABLE downloads ADD COLUMN checksum_algorithm TEXT;
ALTER TABLE downloads ADD COLUMN checksum_value TEXT;
ALTER TABLE downloads ADD COLUMN artifact_owner TEXT;
ALTER TABLE downloads ADD COLUMN artifact_state TEXT NOT NULL DEFAULT 'available' CHECK(artifact_state IN ('available','claimed','consumed'));
CREATE TABLE content_packages (
 id TEXT PRIMARY KEY,
 download_id TEXT NOT NULL UNIQUE REFERENCES downloads(id),
 provider TEXT NOT NULL,
 provider_package_id TEXT NOT NULL,
 content_type TEXT NOT NULL,
 language TEXT NOT NULL,
 variant TEXT NOT NULL,
 title TEXT NOT NULL,
 version TEXT NOT NULL,
 source_url TEXT NOT NULL,
 source_path TEXT NOT NULL,
 local_path TEXT NOT NULL UNIQUE,
 size_bytes INTEGER NOT NULL,
 checksum_algorithm TEXT NOT NULL,
 checksum_value TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('verifying','installing','installed','failed')),
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 installed_at TEXT
);
CREATE UNIQUE INDEX content_packages_reserved_release ON content_packages(provider,provider_package_id,version) WHERE status IN ('verifying','installing','installed');
