-- +goose Up
ALTER TABLE studio_jobs ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE studio_pipeline_templates ADD COLUMN project_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_studio_jobs_project_updated
    ON studio_jobs(project_id, ts_updated DESC);
CREATE INDEX idx_studio_pipeline_templates_project_updated
    ON studio_pipeline_templates(project_id, ts_updated DESC);

-- +goose Down
DROP INDEX idx_studio_pipeline_templates_project_updated;
DROP INDEX idx_studio_jobs_project_updated;
ALTER TABLE studio_pipeline_templates DROP COLUMN project_id;
ALTER TABLE studio_jobs DROP COLUMN project_id;
