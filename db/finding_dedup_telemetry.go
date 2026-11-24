package db

import (
	"log"
	"sync/atomic"
)

// FindingDedupCounters is a bounded, cardinality-safe view of the finding
// deduplication lifecycle. It deliberately contains only aggregate counts — no
// finding IDs, task IDs, titles or evidence payloads — so it can be exposed to
// operators without leaking sensitive content or creating unbounded series.
type FindingDedupCounters struct {
	Submissions        uint64
	New                uint64
	Duplicates         uint64
	EvidenceAttachments uint64
	Conflicts          uint64
	Errors             uint64
}

var findingDedupStats struct {
	submissions         atomic.Uint64
	newFindings         atomic.Uint64
	duplicates          atomic.Uint64
	evidenceAttachments atomic.Uint64
	conflicts           atomic.Uint64
	errors              atomic.Uint64
}

// FindingDedupStats returns a snapshot of the dedup lifecycle counters.
func FindingDedupStats() FindingDedupCounters {
	return FindingDedupCounters{
		Submissions:         findingDedupStats.submissions.Load(),
		New:                 findingDedupStats.newFindings.Load(),
		Duplicates:          findingDedupStats.duplicates.Load(),
		EvidenceAttachments: findingDedupStats.evidenceAttachments.Load(),
		Conflicts:           findingDedupStats.conflicts.Load(),
		Errors:              findingDedupStats.errors.Load(),
	}
}

// recordFindingDedup records one terminal dedup outcome. The log line uses a
// fixed format with only a bounded outcome value and a boolean; it never logs
// user-controlled finding content. Telemetry is strictly informational: it
// cannot fail persistence because it performs only atomic increments.
func recordFindingDedup(outcome string, ok bool) {
	findingDedupStats.submissions.Add(1)
	switch outcome {
	case "new":
		findingDedupStats.newFindings.Add(1)
	case "duplicate":
		findingDedupStats.duplicates.Add(1)
	default:
		findingDedupStats.errors.Add(1)
	}
	log.Printf("[finding-dedup] outcome=%s ok=%t", outcome, ok)
}

// recordFindingDedupConflict records a database-level uniqueness conflict that
// was resolved by reusing the committed logical finding (D3, D5).
func recordFindingDedupConflict() {
	findingDedupStats.conflicts.Add(1)
	log.Printf("[finding-dedup] event=uniqueness_conflict")
}

// recordFindingEvidenceAttachment records that new evidence was bound to an
// existing or new finding. It is called only when AddFindingTrafficTx really
// inserted at least one new binding (duplicate references are not counted).
func recordFindingEvidenceAttachment() {
	findingDedupStats.evidenceAttachments.Add(1)
	log.Printf("[finding-dedup] event=evidence_attached")
}
