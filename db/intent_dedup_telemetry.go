package db

import (
	"log"
	"sync/atomic"
)

// Phase IX.2.2 — intent-management telemetry (Gate K).
//
// Bounded aggregate counters only: no intent summaries, asset ids, task ids or
// model output. Telemetry performs atomic increments and cannot alter planning,
// validation or persistence decisions.

// IntentPlanCounters is a cardinality-safe snapshot of intent management.
type IntentPlanCounters struct {
	Submissions      uint64
	New              uint64
	Duplicates       uint64
	Repeats          uint64
	Retries          uint64
	Retests          uint64
	Rejected         uint64
	BudgetExhausted  uint64
	BlockedDependents uint64
}

var intentPlanStats struct {
	submissions       atomic.Uint64
	newIntents        atomic.Uint64
	duplicates        atomic.Uint64
	repeats           atomic.Uint64
	retries           atomic.Uint64
	retests           atomic.Uint64
	rejected          atomic.Uint64
	budgetExhausted   atomic.Uint64
	blockedDependents atomic.Uint64
}

// IntentPlanStats returns a snapshot of the intent-management counters.
func IntentPlanStats() IntentPlanCounters {
	return IntentPlanCounters{
		Submissions:       intentPlanStats.submissions.Load(),
		New:               intentPlanStats.newIntents.Load(),
		Duplicates:        intentPlanStats.duplicates.Load(),
		Repeats:           intentPlanStats.repeats.Load(),
		Retries:           intentPlanStats.retries.Load(),
		Retests:           intentPlanStats.retests.Load(),
		Rejected:          intentPlanStats.rejected.Load(),
		BudgetExhausted:   intentPlanStats.budgetExhausted.Load(),
		BlockedDependents: intentPlanStats.blockedDependents.Load(),
	}
}

// recordIntentPlan records one accepted proposal outcome.
func recordIntentPlan(outcome string) {
	intentPlanStats.submissions.Add(1)
	switch outcome {
	case IntentOutcomeNew:
		intentPlanStats.newIntents.Add(1)
	case IntentOutcomeDuplicate:
		intentPlanStats.duplicates.Add(1)
	case IntentOutcomeRepeat:
		intentPlanStats.repeats.Add(1)
	case IntentOutcomeRetry:
		intentPlanStats.retries.Add(1)
	case IntentOutcomeRetest:
		intentPlanStats.retests.Add(1)
	}
	log.Printf("[intent-plan] outcome=%s", outcome)
}

// recordIntentReject records a rejected proposal with a bounded reason label.
func recordIntentReject(reason string) {
	intentPlanStats.rejected.Add(1)
	if reason == "budget" {
		intentPlanStats.budgetExhausted.Add(1)
	}
	log.Printf("[intent-plan] event=rejected reason=%s", reason)
}

// recordIntentBlocked records dependents blocked by a failed prerequisite.
func recordIntentBlocked(n int64) {
	if n > 0 {
		intentPlanStats.blockedDependents.Add(uint64(n))
	}
	log.Printf("[intent-plan] event=dependents_blocked count=%d", n)
}
