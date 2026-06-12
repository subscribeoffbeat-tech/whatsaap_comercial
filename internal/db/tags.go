package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tag mirrors the tags table.
type Tag struct {
	ID        int64
	Name      string
	Color     string
	CreatedAt time.Time
}

// ListTags returns all tags ordered by name.
func ListTags(ctx context.Context, pool *pgxpool.Pool) ([]Tag, error) {
	rows, err := pool.Query(ctx, `SELECT id, name, color, created_at FROM tags ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// CreateTag inserts a new tag and returns it with the generated ID.
func CreateTag(ctx context.Context, pool *pgxpool.Pool, name, color string) (*Tag, error) {
	if color == "" {
		color = "#6B7280"
	}
	var t Tag
	err := pool.QueryRow(ctx, `
		INSERT INTO tags (name, color) VALUES ($1, $2)
		RETURNING id, name, color, created_at
	`, name, color).Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return &t, nil
}

// UpdateTag changes a tag's name and/or color.
func UpdateTag(ctx context.Context, pool *pgxpool.Pool, id int64, name, color string) error {
	_, err := pool.Exec(ctx, `UPDATE tags SET name = $2, color = $3 WHERE id = $1`, id, name, color)
	return err
}

// DeleteTag removes a tag (cascades to contact_tags and tag_routing).
func DeleteTag(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM tags WHERE id = $1`, id)
	return err
}
