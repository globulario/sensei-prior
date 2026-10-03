// Package corpus defines the Stage 0 training record (DESIGN.md §8).
//
// Nothing in this package is an authority type. A record describes what
// happened; it never asserts that anything was correct (DESIGN.md §1).
package corpus

import "time"

// LabelState grades how far a reviewer finding can be trusted (DESIGN.md §3).
// It is a closed vocabulary: read it by membership, never by exclusion.
type LabelState string

const (
	ReviewerObservation LabelState = "reviewer_observation"
	FindingUpheld       LabelState = "finding_upheld"
	FindingResolved     LabelState = "finding_resolved"
	FindingDisputed     LabelState = "finding_disputed"
	FindingWithdrawn    LabelState = "finding_withdrawn"
	FindingSuperseded   LabelState = "finding_superseded"
)

// Outcome is how the episode ended, taken from its last terminal event.
type Outcome string

const (
	OutcomeAccepted        Outcome = "ACCEPT"
	OutcomeFailed          Outcome = "FAILED"
	OutcomeBlockedExternal Outcome = "BLOCKED_EXTERNAL"
	OutcomeNonconvergent   Outcome = "NONCONVERGENT"
	OutcomeOpen            Outcome = "OPEN"
)

// SnapshotState says whether the graph an episode was served can be named.
type SnapshotState string

const (
	// SnapshotReferenced names the served generation; the triples are not rebuilt.
	SnapshotReferenced SnapshotState = "REFERENCED"
	// SnapshotUnreconstructable means no served graph can be named. It is never
	// filled from a later graph (DESIGN.md §2).
	SnapshotUnreconstructable SnapshotState = "UNRECONSTRUCTABLE"
)

// Provenance locates the bytes a value was read from.
type Provenance struct {
	Source  string `json:"source"`  // e.g. the events.jsonl path
	Locator string `json:"locator"` // e.g. "line 42"
	Digest  string `json:"digest"`  // sha256 of the bytes read
}

type GraphSnapshot struct {
	State        SnapshotState `json:"state"`
	MarkerDigest string        `json:"marker_digest,omitempty"`
	BuildCommit  string        `json:"build_commit,omitempty"`
	Provenance   *Provenance   `json:"provenance,omitempty"`
}

type PlanAttempt struct {
	AttemptID   string     `json:"attempt_id,omitempty"`
	Source      string     `json:"source,omitempty"` // architect | supplied
	Summary     string     `json:"summary"`
	Files       []string   `json:"files,omitempty"`
	AvailableAt time.Time  `json:"available_at"`
	Provenance  Provenance `json:"provenance"`
}

type Finding struct {
	ID         string     `json:"id,omitempty"`
	Severity   string     `json:"severity,omitempty"`
	Class      string     `json:"class,omitempty"`
	Claim      string     `json:"claim"`
	Reference  string     `json:"reference,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	LabelState LabelState `json:"label_state"`
	// FollowedByAccept records that a later review of a different candidate in
	// the same episode accepted. It is an observation, not a promotion of
	// LabelState (DESIGN.md §9).
	FollowedByAccept bool `json:"followed_by_accept"`
}

type Review struct {
	Decision        string     `json:"decision"` // accept | revise | escalate
	Provider        string     `json:"provider,omitempty"`
	CandidateDigest string     `json:"candidate_digest,omitempty"`
	Findings        []Finding  `json:"findings"`
	AvailableAt     time.Time  `json:"available_at"`
	Provenance      Provenance `json:"provenance"`
}

type Ruling struct {
	Question    string     `json:"question,omitempty"`
	Answer      string     `json:"answer"`
	AvailableAt time.Time  `json:"available_at"`
	Provenance  Provenance `json:"provenance"`
}

// Synthetic classifies commissioning runs. Predicate names the rule that
// decided it, so the classification can be audited and changed.
type Synthetic struct {
	Synthetic bool   `json:"synthetic"`
	Predicate string `json:"predicate"`
}

type Record struct {
	RecordID        string        `json:"record_id"`
	ObjectiveID     string        `json:"objective_id"`
	SessionID       string        `json:"session_id"`
	AvailableAt     time.Time     `json:"available_at"` // when the objective was created
	BaseCommit      string        `json:"base_commit,omitempty"`
	CandidateCommit string        `json:"candidate_commit,omitempty"`
	GraphSnapshot   GraphSnapshot `json:"graph_snapshot"`
	Objective       string        `json:"objective"`
	PlanAttempts    []PlanAttempt `json:"plan_attempts"`
	Reviews         []Review      `json:"reviews"`
	Rulings         []Ruling      `json:"rulings"`
	Outcome         Outcome       `json:"outcome"`
	Terminal        string        `json:"terminal,omitempty"` // the raw terminal event kind
	Synthetic       Synthetic     `json:"synthetic"`
	Provenance      []Provenance  `json:"provenance"`
}

// AsOf returns the record as it could have been known at t: every plan,
// review and ruling that became available after t is removed, and so are the
// episode-level facts that are only known at the end (DESIGN.md §2). Use it to
// build the inputs for a decision point. Never feed a full record to a model as
// the context of one of its own earlier decisions.
func (r Record) AsOf(t time.Time) Record {
	out := r
	out.PlanAttempts = nil
	for _, p := range r.PlanAttempts {
		if !p.AvailableAt.After(t) {
			out.PlanAttempts = append(out.PlanAttempts, p)
		}
	}
	out.Reviews = nil
	for _, rv := range r.Reviews {
		if rv.AvailableAt.After(t) {
			continue
		}
		// FollowedByAccept is knowledge about the future of this finding.
		rv.Findings = append([]Finding(nil), rv.Findings...)
		for i := range rv.Findings {
			rv.Findings[i].FollowedByAccept = false
		}
		out.Reviews = append(out.Reviews, rv)
	}
	out.Rulings = nil
	for _, ru := range r.Rulings {
		if !ru.AvailableAt.After(t) {
			out.Rulings = append(out.Rulings, ru)
		}
	}
	out.Outcome = OutcomeOpen
	out.Terminal = ""
	out.CandidateCommit = ""
	return out
}
