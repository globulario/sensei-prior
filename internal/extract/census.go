package extract

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei-prior/internal/archive"
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
	ObjectiveNumber  corpus.IdentityState `json:"objective_number,omitempty"`
	ObjectiveLabel   string               `json:"objective_label,omitempty"`
	Era              string               `json:"era,omitempty"`
	BoundaryDay      bool                 `json:"boundary_day,omitempty"`
	Sections         []string             `json:"sections,omitempty"`
	Rulings          []int                `json:"rulings,omitempty"`
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

	Findings             int                          `json:"findings"`
	FindingsCorrection   int                          `json:"findings_with_correction"`
	FindingsProofGap     int                          `json:"findings_with_proof_gap"`
	ObjectiveNumbers     map[corpus.IdentityState]int `json:"objective_numbers"`
	Eras                 map[string]int               `json:"eras"`
	BoundaryDay          int                          `json:"boundary_day"`
	SectionRecords       map[string]int               `json:"section_records"` // records containing each section kind
	UnrecognizedHeaders  map[string]int               `json:"unrecognized_headers"`
	RecordsCitingRulings int                          `json:"records_citing_rulings"`
	DistinctRulings      int                          `json:"distinct_rulings"`
}

type Census struct {
	Root     string        `json:"root"`
	Archive  string        `json:"archive,omitempty"`
	Eras     []archive.Era `json:"eras,omitempty"`
	Subjects []Subject     `json:"subjects"`
	// ArchiveRunsWithoutLedger lists archive run files whose task has no
	// extracted record: evidence the archive holds that the ledgers lost.
	ArchiveRunsWithoutLedger []string `json:"archive_runs_without_ledger"`
	// ArchiveRunsExcluded lists archive run files with no usable identity.
	ArchiveRunsExcluded []archive.Excluded `json:"archive_runs_excluded"`
	Counts              Counts             `json:"counts"`
}

const (
	ReasonNoLedger    = "no events.jsonl (session inside a removed worktree, or never written)"
	ReasonNoObjective = "ledger has no task.created event"
	ReasonUnreadable  = "ledger could not be read"
)

// Walk extracts every session under root, enriching each record with the
// archive's authored layer when ix is not nil. It calls emit for each record
// and returns the census. A session that cannot be extracted is an excluded
// subject with its reason, never a silent skip.
func Walk(root string, ix *archive.Index, emit func(corpus.Record) error) (Census, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Census{}, err
	}
	c := Census{Root: root, Subjects: []Subject{}, ArchiveRunsWithoutLedger: []string{}}
	c.ArchiveRunsExcluded = []archive.Excluded{}
	if ix != nil {
		c.Archive, c.Eras = ix.Dir, ix.Eras
		c.ArchiveRunsExcluded = append(c.ArchiveRunsExcluded, ix.Excluded...)
	}
	var findings []corpus.Finding
	var records []corpus.Record
	extracted := map[string]bool{}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		s := Subject{SessionDir: ent.Name()}
		path := filepath.Join(root, ent.Name(), "events.jsonl")
		r, err := Session(path)
		if err == nil {
			Enrich(&r, ix)
		}
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
			s.ObjectiveNumber = r.ObjectiveNumber.State
			s.ObjectiveLabel = r.ObjectiveNumber.Value
			if r.Era != nil {
				s.Era, s.BoundaryDay = r.Era.ID, r.Era.BoundaryDay
			}
			for _, sec := range r.ObjectiveStructure.Sections {
				s.Sections = appendUnique(s.Sections, sec.Kind)
			}
			for _, ru := range r.RulingReferences {
				s.Rulings = append(s.Rulings, ru.Ruling)
			}
			extracted[r.ObjectiveID] = true
			records = append(records, r)
			for _, rv := range r.Reviews {
				findings = append(findings, rv.Findings...)
			}
		}
		c.Subjects = append(c.Subjects, s)
	}
	sort.Slice(c.Subjects, func(i, j int) bool { return c.Subjects[i].SessionDir < c.Subjects[j].SessionDir })
	if ix != nil {
		for _, run := range ix.Runs {
			if !extracted[run.TaskID] {
				c.ArchiveRunsWithoutLedger = append(c.ArchiveRunsWithoutLedger, run.File)
			}
		}
	}
	c.Counts = count(c.Subjects, findings, records)
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

func appendUnique(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

func count(subjects []Subject, findings []corpus.Finding, records []corpus.Record) Counts {
	k := Counts{
		Excluded:            map[string]int{},
		Outcomes:            map[corpus.Outcome]int{},
		FindingLabels:       map[corpus.LabelState]int{},
		ObjectiveNumbers:    map[corpus.IdentityState]int{},
		Eras:                map[string]int{},
		SectionRecords:      map[string]int{},
		UnrecognizedHeaders: map[string]int{},
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
		k.ObjectiveNumbers[s.ObjectiveNumber]++
		if s.Era != "" {
			k.Eras[s.Era]++
		}
		if s.BoundaryDay {
			k.BoundaryDay++
		}
		for _, sec := range s.Sections {
			k.SectionRecords[sec]++
		}
		if len(s.Rulings) > 0 {
			k.RecordsCitingRulings++
		}
	}
	rulings := map[int]bool{}
	for _, r := range records {
		for _, h := range r.ObjectiveStructure.Unrecognized {
			k.UnrecognizedHeaders[h]++
		}
		for _, ru := range r.RulingReferences {
			rulings[ru.Ruling] = true
		}
	}
	k.DistinctRulings = len(rulings)
	for _, f := range findings {
		k.FindingLabels[f.LabelState]++
		k.Findings++
		if f.Correction != "" {
			k.FindingsCorrection++
		}
		if f.ProofGap != "" {
			k.FindingsProofGap++
		}
	}
	return k
}
