-- +goose Up
CREATE TABLE studio_pipeline_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    definition_json TEXT NOT NULL,
    ts_created INTEGER NOT NULL,
    ts_updated INTEGER NOT NULL
);

CREATE INDEX idx_studio_pipeline_templates_updated
    ON studio_pipeline_templates(ts_updated DESC);

-- +goose Down
DROP TABLE studio_pipeline_templates;
