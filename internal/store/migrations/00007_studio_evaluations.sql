-- +goose Up
CREATE TABLE studio_evaluations (
    job_id TEXT PRIMARY KEY REFERENCES studio_jobs(id) ON DELETE CASCADE,
    model_path TEXT NOT NULL,
    mode TEXT NOT NULL,
    metrics_json TEXT NOT NULL DEFAULT '{}',
    parameters_json TEXT NOT NULL DEFAULT '{}',
    ts_created INTEGER NOT NULL
);

CREATE INDEX idx_studio_evaluations_model_created
    ON studio_evaluations(model_path, ts_created DESC);

-- +goose Down
DROP TABLE studio_evaluations;
