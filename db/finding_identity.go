package db

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// FindingIdentityVersion is the version prefix of every canonical finding
// fingerprint. Changing canonicalization requires a NEW version, never a silent
// rewrite of existing fingerprints: old rows keep their v1 value, new rows get
// v2, and the two never collide. This satisfies identity versioning (Gate B5)
// without a destructive full-table rewrite.
const FindingIdentityVersion = "v1"

// normalizeIdentityText applies the documented v1 canonicalization to a single
// text identity component: Unicode case folding, outer trim, and collapse of
// every internal whitespace run to one ASCII space. It deliberately does NOT
// strip punctuation, escape sequences, or numbers — those carry meaningful
// distinctions (parameter names, payload markers, resource identifiers).
func normalizeIdentityText(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// normalizeIdentityAssets canonicalizes the affected-asset set: positive IDs
// only, duplicates removed, numerically sorted, decimal, comma-separated.
// Asset rows are already normalized/unique by the asset subsystem
// (db/assets.go, uq_av2_*), so hostname case, default ports, IPv6 form, path
// and query normalization are delegated there rather than reimplemented here
// (Gate B4). An empty set canonicalizes to "".
func normalizeIdentityAssets(assetIDs []int64) string {
	ids := make([]int64, 0, len(assetIDs))
	seen := make(map[int64]bool, len(assetIDs))
	for _, id := range assetIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// FindingFingerprint returns the canonical, deterministic identity of a finding.
//
// v1 dimensions (the smallest stable set available in the current model):
//   - scope        task_id (per-assessment); 0/negative renders as "legacy"
//   - vulnclass    normalized finding type
//   - name         normalized human title; producers must encode the resource,
//                  parameter and method distinction here (see identity spec)
//   - asset_ids    normalized affected-asset set (structural target identity)
//
// EXCLUDED because they are mutable or provenance, not identity: severity
// (SetFindingSeverity), summary, evidence, worker, status, report, created_at,
// node_id, intent_id, traffic bindings, evidence version, identity version
// itself is a fixed prefix, not a variable input.
//
// Two findings with different evidence but the same four dimensions share one
// identity; their evidence is attached as additional observations instead.
// FindingNameOrDefault returns the finding identity name. When a producer
// omits `name`, the summary is the only place the endpoint/parameter detail
// lives — using it here keeps otherwise distinct findings distinct (IX.2.1.1).
func FindingNameOrDefault(name, summary string) string {
	if s := strings.TrimSpace(name); s != "" {
		return s
	}
	return strings.TrimSpace(summary)
}

func FindingFingerprint(taskID int64, vulnClass, name string, assetIDs []int64) string {
	scope := "legacy"
	if taskID > 0 {
		scope = strconv.FormatInt(taskID, 10)
	}
	parts := []string{
		FindingIdentityVersion,
		scope,
		normalizeIdentityText(vulnClass),
		normalizeIdentityText(name),
		normalizeIdentityAssets(assetIDs),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return FindingIdentityVersion + ":" + hex.EncodeToString(sum[:])
}
