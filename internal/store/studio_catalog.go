package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type StudioCatalogArtifact struct {
	Name         string
	Path         string
	Size         int64
	Kind         string
	MetadataJSON string
	JobID        string
	Operation    string
	Input        string
	CreatedAt    time.Time
	SHA256       string
	GGUFValid    *bool
	VerifyError  string
	TagsJSON     string
	Notes        string
	VerifiedAt   *time.Time
	Registered   bool
}

type StudioArtifactAnnotation struct {
	Path        string
	SHA256      string
	GGUFValid   *bool
	VerifyError string
	TagsJSON    string
	Notes       string
	VerifiedAt  *time.Time
	UpdatedAt   time.Time
}

type StudioLineageRecord struct {
	JobID     string
	Input     string
	Output    string
	Relation  string
	CreatedAt time.Time
}

func (s *Store) ListStudioCatalogArtifacts(ctx context.Context, limit int, kind string) ([]StudioCatalogArtifact, error) {
	if limit <= 0 || limit > 10000 {
		limit = 250
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT a.name, a.path, a.size, a.kind, a.metadata_json,
			       j.id AS job_id, j.operation, j.input_path, j.ts_created,
			       ROW_NUMBER() OVER (
				   PARTITION BY a.path
				   ORDER BY CASE WHEN j.operation IN ('pipeline', 'register') THEN 1 ELSE 0 END, j.ts_updated DESC
			   ) AS rank
			FROM studio_artifacts a
			JOIN studio_jobs j ON j.id = a.job_id
			WHERE j.state = 'completed' AND (? = '' OR a.kind = ?)
		)
		SELECT r.name, r.path, r.size, r.kind, r.metadata_json, r.job_id, r.operation, r.input_path, r.ts_created,
		       COALESCE(n.sha256, ''), n.gguf_valid, COALESCE(n.verification_error, ''),
		       COALESCE(n.tags_json, '[]'), COALESCE(n.notes, ''), n.ts_verified,
		       EXISTS(SELECT 1 FROM studio_artifacts registered
		              WHERE registered.path = r.path AND registered.kind = 'served-model')
		FROM ranked r LEFT JOIN studio_artifact_annotations n ON n.path = r.path
		WHERE r.rank = 1 ORDER BY r.ts_created DESC LIMIT ?`, kind, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("list Studio artifact catalog: %w", err)
	}
	defer rows.Close()
	var artifacts []StudioCatalogArtifact
	for rows.Next() {
		var artifact StudioCatalogArtifact
		var created int64
		var valid sql.NullBool
		var verified sql.NullInt64
		if err := rows.Scan(&artifact.Name, &artifact.Path, &artifact.Size, &artifact.Kind,
			&artifact.MetadataJSON, &artifact.JobID, &artifact.Operation, &artifact.Input, &created,
			&artifact.SHA256, &valid, &artifact.VerifyError, &artifact.TagsJSON, &artifact.Notes, &verified,
			&artifact.Registered); err != nil {
			return nil, fmt.Errorf("scan Studio catalog artifact: %w", err)
		}
		artifact.CreatedAt = time.UnixMilli(created)
		if valid.Valid {
			value := valid.Bool
			artifact.GGUFValid = &value
		}
		if verified.Valid {
			value := time.UnixMilli(verified.Int64)
			artifact.VerifiedAt = &value
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *Store) SaveStudioArtifactAnnotation(ctx context.Context, annotation StudioArtifactAnnotation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO studio_artifact_annotations
			(path, sha256, gguf_valid, verification_error, tags_json, notes, ts_verified, ts_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			sha256=excluded.sha256, gguf_valid=excluded.gguf_valid,
			verification_error=excluded.verification_error, tags_json=excluded.tags_json,
			notes=excluded.notes, ts_verified=excluded.ts_verified, ts_updated=excluded.ts_updated`,
		annotation.Path, annotation.SHA256, nullableBool(annotation.GGUFValid), annotation.VerifyError,
		annotation.TagsJSON, annotation.Notes, nullableTimeMillis(annotation.VerifiedAt), annotation.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("save Studio artifact annotation: %w", err)
	}
	return nil
}

func (s *Store) GetStudioArtifactAnnotation(ctx context.Context, path string) (StudioArtifactAnnotation, error) {
	var annotation StudioArtifactAnnotation
	var valid sql.NullBool
	var verified sql.NullInt64
	var updated int64
	err := s.db.QueryRowContext(ctx, `
		SELECT path, sha256, gguf_valid, verification_error, tags_json, notes, ts_verified, ts_updated
		FROM studio_artifact_annotations WHERE path = ?`, path).Scan(
		&annotation.Path, &annotation.SHA256, &valid, &annotation.VerifyError,
		&annotation.TagsJSON, &annotation.Notes, &verified, &updated)
	if err == sql.ErrNoRows {
		annotation.Path = path
		annotation.TagsJSON = "[]"
		return annotation, nil
	}
	if err != nil {
		return annotation, fmt.Errorf("get Studio artifact annotation: %w", err)
	}
	if valid.Valid {
		value := valid.Bool
		annotation.GGUFValid = &value
	}
	if verified.Valid {
		value := time.UnixMilli(verified.Int64)
		annotation.VerifiedAt = &value
	}
	annotation.UpdatedAt = time.UnixMilli(updated)
	return annotation, nil
}

func (s *Store) StudioArtifactExists(ctx context.Context, path string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM studio_artifacts WHERE path = ?)`, path).Scan(&exists)
	return exists, err
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func (s *Store) ListStudioLineage(ctx context.Context) ([]StudioLineageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.job_id, l.input_path, l.output_path, l.relation, j.ts_created
		FROM studio_lineage l JOIN studio_jobs j ON j.id = l.job_id
		WHERE j.state = 'completed' ORDER BY j.ts_created`)
	if err != nil {
		return nil, fmt.Errorf("list Studio lineage: %w", err)
	}
	defer rows.Close()
	var lineage []StudioLineageRecord
	for rows.Next() {
		var edge StudioLineageRecord
		var created int64
		if err := rows.Scan(&edge.JobID, &edge.Input, &edge.Output, &edge.Relation, &created); err != nil {
			return nil, fmt.Errorf("scan Studio lineage: %w", err)
		}
		edge.CreatedAt = time.UnixMilli(created)
		lineage = append(lineage, edge)
	}
	return lineage, rows.Err()
}
