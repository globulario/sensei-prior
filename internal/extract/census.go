package extract

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei-prior/internal/corpus"
)

// Subject is one session directory and what happened to it. A census lists its
// subjects; the counts are derived from this list, never kept separately.
type Subject struct {
	SessionDir       string               `json:"session_dir"`
	Included         bool                 `json:"included"`
	Reason           string               `json:"reason,omitempty"` // why excluded
	RecordID         string               `json:"record_id,omitempty"`
	Synthetic        bool                 `json:"synthetic,omitempty"`
	Outcome          corpus.Outcome       `json:"outcome,omitempty"`
	ReviseThenAccept bool                 `json:"revise_then_accept,omitempty"`
	Snapshot         corpus.SnapshotState `json:"snapshot,omitempty"`
}

type Counts struct {
	Sessions             int                       `json:"sessions"`
	Included             int                       `json:"included"`
	Excluded             map[string]int            `json:"excluded"`
	Synthetic            int                       `json:"synthetic"`
	ReviseThenAccept     int                       `json:"revise_then_accept"`
	ReviseThenAcceptReal int                       `json:"revise_then_accept_real"`
	SnapshotReferenced   int                       `json:"snapshot_referenced"`
	Outcomes             map[corpus.Outcome]int    `json:"outcomes"`
	FindingLabels        map[corpus.LabelState]int `json:"finding_labels"`
}

type Census struct {
	Root     string    `json:"root"`
	Subjects []Subject `json:"subjects"`
	Counts   Counts    `json:"counts"`
}

const (
	ReasonNoLedger    = "no events.jsonl (session inside a removed worktree, or never written)"
	ReasonNoObjective = "ledger has no task.created event"
	ReasonUnreadable  = "ledger could not be read"
)

// Walk extracts every session under root. It calls emit for each record and
// returns the census. A session that cannot be extracted is an excluded
// subject with its reason, never a silent skip.
func Walk(root string, emit func(corpus.Record) error) (Census, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Census{}, err
	}
	c := Census{Root: root, Subjects: []Subject{}}
	var findings []corpus.Finding
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		s := Subject{SessionDir: ent.Name()}
		path := filepath.Join(root, ent.Name(), "events.jsonl")
		r, err := Session(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			s.Reason = ReasonNoLedger
		case errors.Is(err, ErrNoObjective):
			s.Reason = ReasonNoObjective
		case err != nil:
			s.Reason = ReasonUnreadable + ": " + err.Error()
		default:
			if err := emit(r); err != nil {
				return Census{}, err
			}
			s.Included = true
			s.RecordID = r.RecordID
			s.Synthetic = r.Synthetic.Synthetic
			s.Outcome = r.Outcome
			s.Snapshot = r.GraphSnapshot.State
			s.ReviseThenAccept = reviseThenAccept(r.Reviews)
			for _, rv := range r.Reviews {
				findings = append(findings, rv.Findings...)
			}
		}
		c.Subjects = append(c.Subjects, s)
	}
	sort.Slice(c.Subjects, func(i, j int) bool { return c.Subjects[i].SessionDir < c.Subjects[j].SessionDir })
	c.Counts = count(c.Subjects, findings)
	return c, nil
}

func reviseThenAccept(reviews []corpus.Review) bool {
	revised := false
	for _, rv := range reviews {
		switch rv.Decision {
		case "revise":
			revised = true
		case "accept":
			if revised {
				return true
			}
		}
	}
	return false
}

func count(subjects []Subject, findings []corpus.Finding) Counts {
	k := Counts{
		Excluded:      map[string]int{},
		Outcomes:      map[corpus.Outcome]int{},
		FindingLabels: map[corpus.LabelState]int{},
	}
	for _, s := range subjects {
		k.Sessions++
		if !s.Included {
			k.Excluded[s.Reason]++
			continue
		}
		k.Included++
		k.Outcomes[s.Outcome]++
		if s.Synthetic {
			k.Synthetic++
		}
		if s.ReviseThenAccept {
			k.ReviseThenAccept++
			if !s.Synthetic {
				k.ReviseThenAcceptReal++
			}
		}
		if s.Snapshot == corpus.SnapshotReferenced {
			k.SnapshotReferenced++
		}
	}
	for _, f := range findings {
		k.FindingLabels[f.LabelState]++
	}
	return k
}
