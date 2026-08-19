package store

import (
	"context"
	"fmt"
	"time"
)

type StudioPipelineTemplateRecord struct {
	ID             string
	Name           string
	DefinitionJSON string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *Store) SaveStudioPipelineTemplate(ctx context.Context, template StudioPipelineTemplateRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO studio_pipeline_templates (id, name, definition_json, ts_created, ts_updated)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, definition_json=excluded.definition_json, ts_updated=excluded.ts_updated`,
		template.ID, template.Name, template.DefinitionJSON, template.CreatedAt.UnixMilli(), template.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("save Studio pipeline template: %w", err)
	}
	return nil
}

func (s *Store) ListStudioPipelineTemplates(ctx context.Context) ([]StudioPipelineTemplateRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, definition_json, ts_created, ts_updated
		FROM studio_pipeline_templates ORDER BY ts_updated DESC`)
	if err != nil {
		return nil, fmt.Errorf("list Studio pipeline templates: %w", err)
	}
	defer rows.Close()
	var templates []StudioPipelineTemplateRecord
	for rows.Next() {
		var template StudioPipelineTemplateRecord
		var created, updated int64
		if err := rows.Scan(&template.ID, &template.Name, &template.DefinitionJSON, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan Studio pipeline template: %w", err)
		}
		template.CreatedAt = time.UnixMilli(created)
		template.UpdatedAt = time.UnixMilli(updated)
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Studio pipeline template rows: %w", err)
	}
	return templates, nil
}

func (s *Store) DeleteStudioPipelineTemplate(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM studio_pipeline_templates WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete Studio pipeline template: %w", err)
	}
	count, err := result.RowsAffected()
	return count > 0, err
}
