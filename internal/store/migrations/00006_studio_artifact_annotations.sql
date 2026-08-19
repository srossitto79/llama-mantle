-- +goose Up
CREATE TABLE studio_artifact_annotations (
    path TEXT PRIMARY KEY,
    sha256 TEXT NOT NULL DEFAULT '',
    gguf_valid INTEGER,
    verification_error TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    notes TEXT NOT NULL DEFAULT '',
    ts_verified INTEGER,
    ts_updated INTEGER NOT NULL
);

-- +goose Down
DROP TABLE studio_artifact_annotations;
