package db

import (
	"context"
	"regexp"
	"strings"
)

// likeContains builds a case-insensitive substring pattern with LIKE wildcards
// in the needle escaped, so an id containing % or _ cannot widen the search.
func likeContains(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

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
		patterns = append(patterns, likeContains(a))
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

// ActivityCitesTrafficID reports whether this exploration's recorded tool
// activity actually used or saw the exact traffic id. Traffic ids are derived
// from a unix timestamp plus a short sequence, so global existence in the
// traffic store is predictable and is not proof that this task owns the
// exchange. Only activity rows belonging to this exploration are consulted;
// inherited source-task views never copy rows into the current exploration.
func (s *ExplorationStore) ActivityCitesTrafficID(ctx context.Context, id string) (bool, error) {
	id = strings.TrimSpace(id)
	if s == nil || s.db == nil || id == "" {
		return false, nil
	}
	// Match the id as a whole token: "1730000000-0001" must not be proven by
	// "1730000000-00010" or by the id embedded in a longer opaque string.
	token, err := regexp.Compile(`(?:^|[^0-9A-Za-z_-])` + regexp.QuoteMeta(id) + `(?:[^0-9A-Za-z_-]|$)`)
	if err != nil {
		return false, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT COALESCE(detail,''), COALESCE(metadata::text,'') FROM activity
		  WHERE exploration_id=$1 AND kind IN ('tool_use','tool_result')
		    AND (detail ILIKE $2 OR metadata::text ILIKE $2)
		  ORDER BY id DESC LIMIT 200`,
		s.expID, likeContains(id))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var detail, metadata string
		if err := rows.Scan(&detail, &metadata); err != nil {
			return false, err
		}
		if token.MatchString(detail) || token.MatchString(metadata) {
			return true, nil
		}
	}
	return false, rows.Err()
}
