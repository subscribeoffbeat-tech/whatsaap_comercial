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
	tid := effectiveTenantID(ctx)
	rows, err := pool.Query(ctx, `SELECT id, name, color, created_at FROM tags WHERE tenant_id = $1::uuid ORDER BY name`, tid)
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
	tid := effectiveTenantID(ctx)
	if color == "" {
		color = "#6B7280"
	}
	var t Tag
	err := pool.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color) VALUES ($1::uuid, $2, $3)
		RETURNING id, name, color, created_at
	`, tid, name, color).Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return &t, nil
}

// UpdateTag changes a tag's name and/or color.
func UpdateTag(ctx context.Context, pool *pgxpool.Pool, id int64, name, color string) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `UPDATE tags SET name = $3, color = $4 WHERE tenant_id = $1::uuid AND id = $2`, tid, id, name, color)
	return err
}

// DeleteTag removes a tag (cascades to contact_tags and tag_routing).
func DeleteTag(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	tid := effectiveTenantID(ctx)
	_, err := pool.Exec(ctx, `DELETE FROM tags WHERE tenant_id = $1::uuid AND id = $2`, tid, id)
	return err
}

// TagWithCount extends Tag with the count of opted-in, non-blocked contacts.
type TagWithCount struct {
	Tag
	OptedInCount int
}

// ListTagsWithOptedInCount returns all tags with the count of opted-in non-blocked
// contacts assigned to each, plus the total opted-in non-blocked contact count.
func ListTagsWithOptedInCount(ctx context.Context, pool *pgxpool.Pool) (tags []TagWithCount, totalOptedIn int, err error) {
	tid := effectiveTenantID(ctx)
	if err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM contacts WHERE tenant_id = $1::uuid AND opted_in = true AND is_blocked = false`,
		tid,
	).Scan(&totalOptedIn); err != nil {
		return nil, 0, fmt.Errorf("count opted-in: %w", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT t.id, t.name, t.color, t.created_at,
		       COUNT(DISTINCT c.id) FILTER (WHERE c.opted_in = true AND c.is_blocked = false)
		FROM tags t
		LEFT JOIN contact_tags ct ON ct.tag_id = t.id
		LEFT JOIN contacts c ON c.id = ct.contact_id
		WHERE t.tenant_id = $1::uuid
		GROUP BY t.id, t.name, t.color, t.created_at
		ORDER BY t.name
	`, tid)
	if err != nil {
		return nil, totalOptedIn, fmt.Errorf("list tags with count: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tc TagWithCount
		if err := rows.Scan(&tc.ID, &tc.Name, &tc.Color, &tc.CreatedAt, &tc.OptedInCount); err != nil {
			return nil, totalOptedIn, err
		}
		tags = append(tags, tc)
	}
	return tags, totalOptedIn, rows.Err()
}
