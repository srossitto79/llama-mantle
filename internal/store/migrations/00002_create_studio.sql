-- +goose Up
CREATE TABLE studio_jobs (
    id TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    state TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    pct INTEGER NOT NULL DEFAULT 0,
    input_path TEXT NOT NULL DEFAULT '',
    output_path TEXT NOT NULL DEFAULT '',
    parameters_json TEXT NOT NULL DEFAULT '{}',
    logs_json TEXT NOT NULL DEFAULT '[]',
    exit_code INTEGER,
    ts_created INTEGER NOT NULL,
    ts_updated INTEGER NOT NULL
);

CREATE INDEX idx_studio_jobs_updated
    ON studio_jobs (ts_updated DESC);

CREATE TABLE studio_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES studio_jobs(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    path TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    kind TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE(job_id, path)
);

CREATE INDEX idx_studio_artifacts_job
    ON studio_artifacts (job_id);

CREATE TABLE studio_lineage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL REFERENCES studio_jobs(id) ON DELETE CASCADE,
    input_path TEXT NOT NULL,
    output_path TEXT NOT NULL,
    relation TEXT NOT NULL,
    UNIQUE(job_id, input_path, output_path)
);

CREATE INDEX idx_studio_lineage_output
    ON studio_lineage (output_path);

-- +goose Down
DROP TABLE studio_lineage;
DROP TABLE studio_artifacts;
DROP TABLE studio_jobs;
