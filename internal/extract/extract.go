// Package extract turns one sensei-code session ledger into a corpus.Record.
//
// It infers nothing. A value the ledger does not state is left empty, and every
// value it does state carries the provenance of the line it came from.
package extract

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/globulario/sensei-prior/internal/corpus"
	"github.com/globulario/sensei-prior/internal/events"
)

// ErrNoObjective means the ledger has no task.created event, so there is no
// episode to reconstruct.
var ErrNoObjective = errors.New("ledger has no task.created event")

// SyntheticPredicate is the rule that marks a commissioning run.
const SyntheticPredicate = "an agent.started event names a stub- agent"

// terminals maps each workflow terminal kind to an outcome. Read by
// membership: a kind missing from this table is not a terminal.
var terminals = map[string]corpus.Outcome{
	"workflow.completed":               corpus.OutcomeAccepted, // narrowed below
	"workflow.failed":                  corpus.OutcomeFailed,
	"workflow.dirty_canonical_refused": corpus.OutcomeFailed,
	"workflow.restoration_refused":     corpus.OutcomeFailed,
	"workflow.blocked_external":        corpus.OutcomeBlockedExternal,
	"workflow.not_converged":           corpus.OutcomeNonconvergent,
	"workflow.awaiting_authority":      corpus.OutcomeOpen,
	"workflow.awaiting_review":         corpus.OutcomeOpen,
}

// Session extracts the record for the ledger at path.
func Session(path string) (corpus.Record, error) {
	evs, err := events.Load(path)
	if err != nil {
		return corpus.Record{}, err
	}
	return fromEvents(path, evs)
}

func prov(path string, e events.Event) corpus.Provenance {
	return corpus.Provenance{Source: path, Locator: "line " + strconv.Itoa(e.Line), Digest: e.Digest}
}

type receiptValue struct {
	Text  string `json:"text"`
	State string `json:"state"`
}

func (v receiptValue) known() string {
	if v.State == "KNOWN" {
		return v.Text
	}
	return ""
}

func fromEvents(path string, evs []events.Event) (corpus.Record, error) {
	var r corpus.Record
	r.GraphSnapshot.State = corpus.SnapshotUnreconstructable
	created := false

	for _, e := range evs {
		switch {
		case e.Kind == "task.created":
			if created {
				continue // one task per session; a second creation is not a new objective
			}
			created = true
			r.ObjectiveID = e.TaskID
			r.RecordID = e.TaskID
			r.SessionID = e.SessionID
			r.Objective = e.Summary
			r.AvailableAt = e.Time
			r.Provenance = append(r.Provenance, prov(path, e))

		case e.Kind == "agent.started":
			if strings.HasPrefix(strings.TrimSpace(e.Summary), "stub-") {
				r.Synthetic = corpus.Synthetic{Synthetic: true, Predicate: SyntheticPredicate}
			}

		case e.Kind == "plan.proposed":
			var p struct {
				AttemptID string   `json:"plan_attempt_id"`
				Source    string   `json:"plan_source"`
				Summary   string   `json:"summary"`
				Files     []string `json:"files"`
			}
			_ = json.Unmarshal(e.Payload, &p)
			summary := p.Summary
			if summary == "" {
				summary = e.Summary
			}
			r.PlanAttempts = append(r.PlanAttempts, corpus.PlanAttempt{
				AttemptID: p.AttemptID, Source: p.Source, Summary: summary,
				Files: p.Files, AvailableAt: e.Time, Provenance: prov(path, e),
			})

		case e.Kind == "review.completed":
			var v struct {
				Decision   string `json:"decision"`
				Provenance struct {
					Provider         string `json:"provider"`
					CandidateDigest  string `json:"candidate_digest"`
					GraphBuildCommit string `json:"graph_build_commit"`
				} `json:"provenance"`
				Findings []struct {
					ID        string `json:"id"`
					Severity  string `json:"severity"`
					Class     string `json:"class"`
					Claim     string `json:"claim"`
					Reference string `json:"reference"`
					Reason    string `json:"reason"`
				} `json:"findings"`
			}
			if err := json.Unmarshal(e.Payload, &v); err != nil || v.Decision == "" {
				continue // a verdict that cannot be read is not a verdict
			}
			rv := corpus.Review{
				Decision: v.Decision, Provider: v.Provenance.Provider,
				CandidateDigest: v.Provenance.CandidateDigest,
				Findings:        []corpus.Finding{}, AvailableAt: e.Time, Provenance: prov(path, e),
			}
			for _, f := range v.Findings {
				rv.Findings = append(rv.Findings, corpus.Finding{
					ID: f.ID, Severity: f.Severity, Class: f.Class, Claim: f.Claim,
					Reference: f.Reference, Reason: f.Reason,
					// No finding lifecycle exists in the ledger (DESIGN.md §9):
					// every finding is an observation and is never promoted.
					LabelState: corpus.ReviewerObservation,
				})
			}
			if r.GraphSnapshot.BuildCommit == "" {
				r.GraphSnapshot.BuildCommit = v.Provenance.GraphBuildCommit
			}
			r.Reviews = append(r.Reviews, rv)

		case e.Kind == "authority.resolved":
			var a struct {
				Question    string `json:"question"`
				OptionLabel string `json:"option_label"`
				OptionID    string `json:"option_id"`
				Option      string `json:"option"`
			}
			_ = json.Unmarshal(e.Payload, &a)
			answer := firstNonEmpty(a.OptionLabel, a.OptionID, a.Option)
			if answer == "" {
				continue
			}
			r.Rulings = append(r.Rulings, corpus.Ruling{
				Question: a.Question, Answer: answer, AvailableAt: e.Time, Provenance: prov(path, e),
			})

		case e.Kind == "run.receipt":
			var p struct {
				Receipt struct {
					BaseCommit      receiptValue `json:"base_commit"`
					GraphDigest     receiptValue `json:"graph_digest"`
					CandidateCommit receiptValue `json:"candidate_commit"`
				} `json:"receipt"`
			}
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			// The last receipt describes the run as it finally stood.
			if v := p.Receipt.BaseCommit.known(); v != "" {
				r.BaseCommit = v
			}
			if v := p.Receipt.CandidateCommit.known(); v != "" {
				r.CandidateCommit = v
			}
			if v := p.Receipt.GraphDigest.known(); v != "" {
				pv := prov(path, e)
				r.GraphSnapshot.State = corpus.SnapshotReferenced
				r.GraphSnapshot.MarkerDigest = v
				r.GraphSnapshot.Provenance = &pv
			}

		default:
			if _, ok := terminals[e.Kind]; ok {
				r.Terminal = e.Kind // resumes append; the last terminal wins
			}
		}
	}
	if !created {
		return corpus.Record{}, ErrNoObjective
	}

	r.Outcome = outcome(r)
	markFollowedByAccept(r.Reviews)
	if r.PlanAttempts == nil {
		r.PlanAttempts = []corpus.PlanAttempt{}
	}
	if r.Reviews == nil {
		r.Reviews = []corpus.Review{}
	}
	if r.Rulings == nil {
		r.Rulings = []corpus.Ruling{}
	}
	return r, nil
}

// outcome derives the episode outcome. ACCEPT requires both a completed
// terminal and an accepting final review: a run that completed without one is
// not a candidate_accepted label, so it stays OPEN.
func outcome(r corpus.Record) corpus.Outcome {
	o, ok := terminals[r.Terminal]
	if !ok {
		return corpus.OutcomeOpen
	}
	if o == corpus.OutcomeAccepted {
		if n := len(r.Reviews); n == 0 || r.Reviews[n-1].Decision != "accept" {
			return corpus.OutcomeOpen
		}
	}
	return o
}

// markFollowedByAccept sets FollowedByAccept on every finding of a review that
// a later review of a different candidate accepted.
func markFollowedByAccept(reviews []corpus.Review) {
	for i := range reviews {
		for j := i + 1; j < len(reviews); j++ {
			if reviews[j].Decision == "accept" && reviews[j].CandidateDigest != reviews[i].CandidateDigest {
				for k := range reviews[i].Findings {
					reviews[i].Findings[k].FollowedByAccept = true
				}
				break
			}
		}
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
