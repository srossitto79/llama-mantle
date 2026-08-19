-- +goose Up
DELETE FROM studio_lineage;
INSERT OR IGNORE INTO studio_lineage (job_id, input_path, output_path, relation)
SELECT j.id, j.input_path, a.path, j.operation
FROM studio_jobs j
JOIN studio_artifacts a ON a.job_id = j.id
WHERE j.input_path <> '' AND a.path <> '';

-- +goose Down
DELETE FROM studio_lineage;
INSERT OR IGNORE INTO studio_lineage (job_id, input_path, output_path, relation)
SELECT id, input_path, output_path, operation
FROM studio_jobs
WHERE input_path <> '' AND output_path <> '';
