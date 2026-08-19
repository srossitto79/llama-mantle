package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// StudioJobRecord is the durable representation of a Studio task.
type StudioJobRecord struct {
	ID             string
	Operation      string
	State          string
	Message        string
	Pct            int
	Input          string
	Output         string
	ParametersJSON string
	LogsJSON       string
	ExitCode       *int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	JobClass       string
	QueuedAt       *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	Artifacts      []StudioArtifactRecord
}

type StudioArtifactRecord struct {
	Name         string
	Path         string
	Size         int64
	Kind         string
	MetadataJSON string
}

// SaveStudioJob atomically saves a job snapshot, its artifacts, and lineage.
func (s *Store) SaveStudioJob(ctx context.Context, job StudioJobRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save studio job: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO studio_jobs (
			id, operation, state, message, pct, input_path, output_path,
			parameters_json, logs_json, exit_code, ts_created, ts_updated,
			job_class, ts_queued, ts_started, ts_finished
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			operation=excluded.operation, state=excluded.state, message=excluded.message,
			pct=excluded.pct, input_path=excluded.input_path, output_path=excluded.output_path,
			parameters_json=excluded.parameters_json, logs_json=excluded.logs_json,
			exit_code=excluded.exit_code, ts_updated=excluded.ts_updated,
			job_class=excluded.job_class, ts_queued=excluded.ts_queued,
			ts_started=excluded.ts_started, ts_finished=excluded.ts_finished`,
		job.ID, job.Operation, job.State, job.Message, job.Pct, job.Input, job.Output,
		job.ParametersJSON, job.LogsJSON, job.ExitCode, job.CreatedAt.UnixMilli(), job.UpdatedAt.UnixMilli(),
		job.JobClass, nullableTimeMillis(job.QueuedAt), nullableTimeMillis(job.StartedAt), nullableTimeMillis(job.FinishedAt))
	if err != nil {
		return fmt.Errorf("upsert studio job: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM studio_artifacts WHERE job_id = ?`, job.ID); err != nil {
		return fmt.Errorf("replace studio artifacts: %w", err)
	}
	for _, artifact := range job.Artifacts {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO studio_artifacts (job_id, name, path, size, kind, metadata_json)
			VALUES (?, ?, ?, ?, ?, ?)`, job.ID, artifact.Name, artifact.Path, artifact.Size,
			artifact.Kind, artifact.MetadataJSON); err != nil {
			return fmt.Errorf("insert studio artifact: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM studio_lineage WHERE job_id = ?`, job.ID); err != nil {
		return fmt.Errorf("replace studio lineage: %w", err)
	}
	if job.Input != "" {
		for _, artifact := range job.Artifacts {
			if artifact.Path == "" {
				continue
			}
			if _, err = tx.ExecContext(ctx, `
				INSERT INTO studio_lineage (job_id, input_path, output_path, relation)
				VALUES (?, ?, ?, ?)`, job.ID, job.Input, artifact.Path, job.Operation); err != nil {
				return fmt.Errorf("insert studio lineage: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit studio job: %w", err)
	}
	return nil
}

// RecoverStudioJobs marks work that was running when the process stopped.
func (s *Store) RecoverStudioJobs(ctx context.Context) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		UPDATE studio_jobs
		SET state = 'failed', message = 'Interrupted by process restart', ts_updated = ?, ts_finished = ?
		WHERE state IN ('running', 'queued')`, now, now)
	if err != nil {
		return fmt.Errorf("recover studio jobs: %w", err)
	}
	return nil
}

// ListStudioJobs returns the most recently updated durable jobs.
func (s *Store) ListStudioJobs(ctx context.Context, limit int) ([]StudioJobRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, operation, state, message, pct, input_path, output_path,
		       parameters_json, logs_json, exit_code, ts_created, ts_updated,
		       job_class, ts_queued, ts_started, ts_finished
		FROM studio_jobs ORDER BY ts_updated DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list studio jobs: %w", err)
	}
	defer rows.Close()

	var jobs []StudioJobRecord
	for rows.Next() {
		var job StudioJobRecord
		var exitCode sql.NullInt64
		var created, updated int64
		var queued, started, finished sql.NullInt64
		if err := rows.Scan(&job.ID, &job.Operation, &job.State, &job.Message, &job.Pct,
			&job.Input, &job.Output, &job.ParametersJSON, &job.LogsJSON, &exitCode,
			&created, &updated, &job.JobClass, &queued, &started, &finished); err != nil {
			return nil, fmt.Errorf("scan studio job: %w", err)
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			job.ExitCode = &code
		}
		job.CreatedAt = time.UnixMilli(created)
		job.UpdatedAt = time.UnixMilli(updated)
		job.QueuedAt = timeFromNullMillis(queued)
		job.StartedAt = timeFromNullMillis(started)
		job.FinishedAt = timeFromNullMillis(finished)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list studio jobs rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close studio jobs rows: %w", err)
	}
	for i := range jobs {
		jobs[i].Artifacts, err = s.listStudioArtifacts(ctx, jobs[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

func nullableTimeMillis(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixMilli()
}

func timeFromNullMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64)
	return &result
}

func (s *Store) listStudioArtifacts(ctx context.Context, jobID string) ([]StudioArtifactRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, path, size, kind, metadata_json
		FROM studio_artifacts WHERE job_id = ? ORDER BY id`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list studio artifacts: %w", err)
	}
	defer rows.Close()
	var artifacts []StudioArtifactRecord
	for rows.Next() {
		var artifact StudioArtifactRecord
		if err := rows.Scan(&artifact.Name, &artifact.Path, &artifact.Size, &artifact.Kind,
			&artifact.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan studio artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}
