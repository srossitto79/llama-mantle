-- +goose Up
ALTER TABLE studio_jobs ADD COLUMN job_class TEXT NOT NULL DEFAULT '';
ALTER TABLE studio_jobs ADD COLUMN ts_queued INTEGER;
ALTER TABLE studio_jobs ADD COLUMN ts_started INTEGER;
ALTER TABLE studio_jobs ADD COLUMN ts_finished INTEGER;

-- +goose Down
ALTER TABLE studio_jobs DROP COLUMN ts_finished;
ALTER TABLE studio_jobs DROP COLUMN ts_started;
ALTER TABLE studio_jobs DROP COLUMN ts_queued;
ALTER TABLE studio_jobs DROP COLUMN job_class;
