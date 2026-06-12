package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SegmentFilter describes the criteria for a saved segment.
type SegmentFilter struct {
	Tags              []int64 `json:"tags,omitempty"`
	TagOp             string  `json:"tag_op,omitempty"` // "all" | "any" (default "any")
	OptedIn           *bool   `json:"opted_in,omitempty"`
	ClickedCampaignID *string `json:"clicked_campaign_id,omitempty"`
}

// Segment mirrors the saved_segments table.
type Segment struct {
	ID        int64
	Name      string
	Filter    SegmentFilter
	CreatedAt time.Time
}

// ListSegments returns all saved segments ordered by name.
func ListSegments(ctx context.Context, pool *pgxpool.Pool) ([]Segment, error) {
	rows, err := pool.Query(ctx, `SELECT id, name, filter::text, created_at FROM saved_segments ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list segments: %w", err)
	}
	defer rows.Close()

	var segs []Segment
	for rows.Next() {
		var s Segment
		var filterRaw string
		if err := rows.Scan(&s.ID, &s.Name, &filterRaw, &s.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(filterRaw), &s.Filter)
		segs = append(segs, s)
	}
	return segs, rows.Err()
}

// CreateSegment inserts a new saved segment and populates s.ID and s.CreatedAt.
func CreateSegment(ctx context.Context, pool *pgxpool.Pool, s *Segment) error {
	filterJSON, _ := json.Marshal(s.Filter)
	return pool.QueryRow(ctx, `
		INSERT INTO saved_segments (name, filter)
		VALUES ($1, $2::jsonb)
		RETURNING id, created_at
	`, s.Name, string(filterJSON)).Scan(&s.ID, &s.CreatedAt)
}

// DeleteSegment removes a saved segment by ID.
func DeleteSegment(ctx context.Context, pool *pgxpool.Pool, id int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM saved_segments WHERE id = $1`, id)
	return err
}

// ResolveSegment evaluates the segment filter and returns matching contact IDs.
func ResolveSegment(ctx context.Context, pool *pgxpool.Pool, segmentID int64) ([]string, error) {
	var filterRaw string
	err := pool.QueryRow(ctx, `SELECT filter::text FROM saved_segments WHERE id = $1`, segmentID).Scan(&filterRaw)
	if err != nil {
		return nil, fmt.Errorf("get segment: %w", err)
	}
	var f SegmentFilter
	_ = json.Unmarshal([]byte(filterRaw), &f)
	return ResolveFilter(ctx, pool, f)
}

// ResolveFilter evaluates a SegmentFilter directly (used by campaign builder).
func ResolveFilter(ctx context.Context, pool *pgxpool.Pool, f SegmentFilter) ([]string, error) {
	conds := []string{"TRUE"}
	args := []any{}
	n := 1

	if f.OptedIn != nil {
		conds = append(conds, fmt.Sprintf("c.opted_in = $%d", n))
		args = append(args, *f.OptedIn)
		n++
	}

	if len(f.Tags) > 0 {
		op := f.TagOp
		if op != "all" {
			op = "any"
		}
		if op == "all" {
			conds = append(conds, fmt.Sprintf(
				"(SELECT COUNT(*) FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = ANY($%d::bigint[])) = $%d",
				n, n+1,
			))
			args = append(args, f.Tags, int64(len(f.Tags)))
			n += 2
		} else {
			conds = append(conds, fmt.Sprintf(
				"EXISTS (SELECT 1 FROM contact_tags ct WHERE ct.contact_id = c.id AND ct.tag_id = ANY($%d::bigint[]))",
				n,
			))
			args = append(args, f.Tags)
			n++
		}
	}

	if f.ClickedCampaignID != nil {
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM click_tracking ck WHERE ck.contact_id = c.id AND ck.campaign_id = $%d::uuid)",
			n,
		))
		args = append(args, *f.ClickedCampaignID)
		n++
	}

	query := fmt.Sprintf(`SELECT c.id::text FROM contacts c WHERE %s`, strings.Join(conds, " AND "))
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
