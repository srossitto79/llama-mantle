-- +goose Up
CREATE TABLE studio_projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    ts_created INTEGER NOT NULL,
    ts_updated INTEGER NOT NULL
);

CREATE TABLE studio_project_resources (
    project_id TEXT NOT NULL REFERENCES studio_projects(id) ON DELETE CASCADE,
    resource_path TEXT NOT NULL,
    PRIMARY KEY (project_id, resource_path)
);

CREATE INDEX idx_studio_project_resources_path ON studio_project_resources(resource_path);

-- +goose Down
DROP TABLE studio_project_resources;
DROP TABLE studio_projects;
