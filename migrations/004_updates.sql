CREATE TABLE active_packages (
 package_id TEXT PRIMARY KEY REFERENCES content_packages(id)
);
INSERT INTO active_packages(package_id) SELECT id FROM content_packages WHERE status='installed';
CREATE TABLE package_updates (
 id TEXT PRIMARY KEY,
 package_id TEXT NOT NULL REFERENCES content_packages(id),
 candidate_provider_package_id TEXT NOT NULL,
 installed_version TEXT NOT NULL,
 candidate_version TEXT NOT NULL,
 size_bytes INTEGER,
 download_id TEXT REFERENCES downloads(id),
 state TEXT NOT NULL CHECK(state IN ('creating','downloading','ready','activating','succeeded','failed','interrupted','cancelled')),
 error_code TEXT NOT NULL DEFAULT '',
 error_message TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX package_updates_live ON package_updates(package_id) WHERE state NOT IN ('succeeded','cancelled');
