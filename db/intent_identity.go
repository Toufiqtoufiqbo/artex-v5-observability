package db

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Phase IX.2.2 — canonical intent identity (Gate B).
//
// An "intent" is one unit of work the planner proposes: what to test, on which
// resource, against which assets. The identity below answers exactly one
// question: "is this the same piece of work as one already in this task?"
//
// Scope is the exploration (one task = one exploration). Identical wording in
// two independent assessments is never merged: the exploration id is part of
// the digest.
//
// The identity deliberately EXCLUDES:
//   - priority       (scheduling hint, mutable)
//   - parent_ids     (provenance: the same work derived from two facts is one
//                     unit of work)
//   - state/owner    (lifecycle)
//   - timestamps     (presentation)
//
// It deliberately INCLUDES:
//   - structured operation + resource when the producer supplies them, so that
//     an equivalent intent expressed in different words collapses to the same
//     digest;
//   - the sorted, de-duplicated asset id set, so "same operation, different
//     target" stays distinct;
//   - otherwise, the normalised summary text (conservative fallback: case and
//     whitespace differences only — never punctuation, digits, paths or query
//     parameters, because those carry meaning).
const IntentIdentityVersion = "v1"

// IntentIdentityInput is the canonical identity input. Zero values are valid;
// an empty operation/resource pair switches the identity to text mode.
type IntentIdentityInput struct {
	ExplorationID int64
	Summary       string
	Operation     string
	Resource      string
	AssetIDs      []int64
}

// IntentFingerprint returns the versioned, deterministic identity of an intent.
// Two proposals with the same fingerprint within the same exploration are the
// same logical intent.
func IntentFingerprint(in IntentIdentityInput) string {
	parts := []string{IntentIdentityVersion, "exp", strconv.FormatInt(in.ExplorationID, 10)}
	op, res := normalizeIntentText(in.Operation), normalizeIntentText(in.Resource)
	if op != "" || res != "" {
		// Structural mode: the producer named the operation and/or the resource,
		// so wording of the free-text summary must not affect identity.
		parts = append(parts, "op", op, "res", res)
	} else {
		parts = append(parts, "text", normalizeIntentText(in.Summary))
	}
	parts = append(parts, "assets", normalizeIntentAssets(in.AssetIDs))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return IntentIdentityVersion + ":" + hex.EncodeToString(sum[:])
}

// normalizeIntentText lower-cases, trims and collapses internal whitespace runs
// to one ASCII space. Everything else (punctuation, digits, URL paths, query
// parameters, percent escapes) is preserved: those distinguish real work.
func normalizeIntentText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			pendingSpace = true
			continue
		}
		if pendingSpace && b.Len() > 0 {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeIntentAssets keeps positive ids only, de-duplicates, sorts ascending
// and renders them comma-joined, so set order and repetition cannot change the
// identity.
func normalizeIntentAssets(ids []int64) string {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	parts := make([]string, len(out))
	for i, id := range out {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
