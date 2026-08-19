package store

import (
	"context"
	"fmt"
	"time"
)

type StudioProjectRecord struct {
	ID, Name, Description string
	CreatedAt, UpdatedAt  time.Time
	Resources             []string
}

func (s *Store) StudioProjectExists(ctx context.Context, id string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM studio_projects WHERE id=?`, id).Scan(&count); err != nil {
		return false, fmt.Errorf("check Studio project: %w", err)
	}
	return count > 0, nil
}

func (s *Store) SaveStudioProject(ctx context.Context, project StudioProjectRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO studio_projects (id,name,description,ts_created,ts_updated) VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,ts_updated=excluded.ts_updated`, project.ID, project.Name, project.Description, project.CreatedAt.UnixMilli(), project.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("save Studio project: %w", err)
	}
	return nil
}

func (s *Store) ListStudioProjects(ctx context.Context) ([]StudioProjectRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,ts_created,ts_updated FROM studio_projects ORDER BY ts_updated DESC`)
	if err != nil {
		return nil, err
	}
	var projects []StudioProjectRecord
	for rows.Next() {
		var item StudioProjectRecord
		var created, updated int64
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		item.CreatedAt = time.UnixMilli(created)
		item.UpdatedAt = time.UnixMilli(updated)
		projects = append(projects, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range projects {
		resourceRows, err := s.db.QueryContext(ctx, `SELECT resource_path FROM studio_project_resources WHERE project_id=? ORDER BY resource_path`, projects[i].ID)
		if err != nil {
			return nil, err
		}
		for resourceRows.Next() {
			var path string
			if err := resourceRows.Scan(&path); err != nil {
				resourceRows.Close()
				return nil, err
			}
			projects[i].Resources = append(projects[i].Resources, path)
		}
		resourceRows.Close()
	}
	return projects, nil
}

func (s *Store) ReplaceStudioProjectResources(ctx context.Context, projectID string, paths []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM studio_project_resources WHERE project_id=?`, projectID); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err = tx.ExecContext(ctx, `INSERT INTO studio_project_resources(project_id,resource_path) VALUES(?,?)`, projectID, path); err != nil {
			return fmt.Errorf("save project resource: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE studio_projects SET ts_updated=? WHERE id=?`, time.Now().UnixMilli(), projectID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return fmt.Errorf("Studio project %q was not found", projectID)
	}
	return tx.Commit()
}

func (s *Store) DeleteStudioProject(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM studio_projects WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	count, _ := result.RowsAffected()
	return count > 0, nil
}
