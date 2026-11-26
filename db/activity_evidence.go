package db

import (
	"context"
	"strings"
)

// HasToolResultAnchor reports whether ONE successful tool result recorded for
// this exploration contains at least minMatches of the anchors. Counting inside
// a single row prevents a fabricated claim from pairing a real status code from
// one response with a path invented from another. Activity rows are written by
// the harness, not the model, and are keyed by exploration id, so a fact from
// task A cannot borrow task B's result.
func (s *ExplorationStore) HasToolResultAnchor(ctx context.Context, anchors []string, minMatches int) (bool, error) {
	if s == nil || s.db == nil || len(anchors) == 0 {
		return false, nil
	}
	if minMatches <= 0 {
		minMatches = 1
	}
	patterns := make([]string, 0, len(anchors))
	needles := make([]string, 0, len(anchors))
	for _, a := range anchors {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if len(a) > 200 {
			a = a[:200]
		}
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		patterns = append(patterns, "%"+r.Replace(a)+"%")
		needles = append(needles, strings.ToLower(a))
	}
	if len(needles) < minMatches {
		return false, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT detail FROM activity
		  WHERE exploration_id=$1 AND kind='tool_result' AND NOT is_error
		    AND detail ILIKE ANY($2::text[])
		  ORDER BY id DESC LIMIT 100`,
		s.expID, patterns)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			return false, err
		}
		lower := strings.ToLower(detail)
		hits := 0
		for _, n := range needles {
			if strings.Contains(lower, n) {
				hits++
			}
		}
		if hits >= minMatches {
			return true, nil
		}
	}
	return false, rows.Err()
}
