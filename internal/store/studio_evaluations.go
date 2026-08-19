package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type StudioEvaluationRecord struct {
	JobID          string
	ProjectID      string
	Model          string
	Mode           string
	MetricsJSON    string
	ParametersJSON string
	CreatedAt      time.Time
}

func (s *Store) GetStudioEvaluation(ctx context.Context, jobID string) (StudioEvaluationRecord, error) {
	var evaluation StudioEvaluationRecord
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT e.job_id, j.project_id, e.model_path, e.mode, e.metrics_json, e.parameters_json, e.ts_created
		FROM studio_evaluations e JOIN studio_jobs j ON j.id=e.job_id WHERE e.job_id = ?`, jobID).Scan(
		&evaluation.JobID, &evaluation.ProjectID, &evaluation.Model, &evaluation.Mode, &evaluation.MetricsJSON,
		&evaluation.ParametersJSON, &created)
	if err == sql.ErrNoRows {
		return evaluation, fmt.Errorf("evaluation job %q was not found", jobID)
	}
	if err != nil {
		return evaluation, fmt.Errorf("get Studio evaluation: %w", err)
	}
	evaluation.CreatedAt = time.UnixMilli(created)
	return evaluation, nil
}

func (s *Store) SaveStudioEvaluation(ctx context.Context, evaluation StudioEvaluationRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO studio_evaluations (job_id, model_path, mode, metrics_json, parameters_json, ts_created)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET metrics_json=excluded.metrics_json, parameters_json=excluded.parameters_json`,
		evaluation.JobID, evaluation.Model, evaluation.Mode, evaluation.MetricsJSON,
		evaluation.ParametersJSON, evaluation.CreatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("save Studio evaluation: %w", err)
	}
	return nil
}

func (s *Store) ListStudioEvaluations(ctx context.Context, model string, limit int) ([]StudioEvaluationRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.job_id, j.project_id, e.model_path, e.mode, e.metrics_json, e.parameters_json, e.ts_created
		FROM studio_evaluations e JOIN studio_jobs j ON j.id=e.job_id WHERE (? = '' OR e.model_path = ?)
		ORDER BY e.ts_created DESC LIMIT ?`, model, model, limit)
	if err != nil {
		return nil, fmt.Errorf("list Studio evaluations: %w", err)
	}
	defer rows.Close()
	var evaluations []StudioEvaluationRecord
	for rows.Next() {
		var evaluation StudioEvaluationRecord
		var created int64
		if err := rows.Scan(&evaluation.JobID, &evaluation.ProjectID, &evaluation.Model, &evaluation.Mode,
			&evaluation.MetricsJSON, &evaluation.ParametersJSON, &created); err != nil {
			return nil, fmt.Errorf("scan Studio evaluation: %w", err)
		}
		evaluation.CreatedAt = time.UnixMilli(created)
		evaluations = append(evaluations, evaluation)
	}
	return evaluations, rows.Err()
}
