package extract

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei-prior/internal/corpus"
)

// Lines follow the production envelope and payload shapes observed in
// sensei-code session ledgers on 2026-10-03.
const (
	lCreated   = `{"id":"e1","time":"2026-09-19T15:46:57Z","session_id":"s1","task_id":"task-1","source":"engine","kind":"task.created","summary":"Append one comment line to internal/report/report.go","payload":null}`
	lStub      = `{"id":"e2","time":"2026-09-19T15:47:01Z","session_id":"s1","task_id":"task-1","kind":"agent.started","summary":"stub-architect started","payload":null}`
	lReal      = `{"id":"e2","time":"2026-09-19T15:47:01Z","session_id":"s1","task_id":"task-1","kind":"agent.started","summary":"Claude started","payload":null}`
	lPlan      = `{"id":"e3","time":"2026-09-19T15:47:04Z","session_id":"s1","task_id":"task-1","kind":"plan.proposed","summary":"append one comment line","payload":{"plan_source":"architect","summary":"append one comment line","files":["internal/report/report.go"],"mode":"modify","decision":"proceed"}}`
	lRevise    = `{"id":"e4","time":"2026-09-19T15:47:56Z","session_id":"s1","task_id":"task-1","kind":"review.completed","summary":"REVISE","payload":{"decision":"revise","provenance":{"provider":"chatgpt","candidate_digest":"d027","graph_build_commit":"05feaf64"},"findings":[{"id":"1","severity":"blocking","claim":"the change is not proven","reference":"internal/report/report.go","reason":"no witness","correction":"add a witness test","proof_gap":"no test names report.go"}]}}`
	lAccept    = `{"id":"e5","time":"2026-09-19T15:48:28Z","session_id":"s1","task_id":"task-1","kind":"review.completed","summary":"ACCEPT","payload":{"decision":"accept","provenance":{"provider":"chatgpt","candidate_digest":"a698","graph_build_commit":"05feaf64"},"findings":[]}}`
	lReceipt   = `{"id":"e6","time":"2026-09-19T15:48:29Z","session_id":"s1","task_id":"task-1","kind":"run.receipt","summary":"receipt","payload":{"receipt":{"base_commit":{"text":"3b07f93d","state":"KNOWN"},"graph_digest":{"text":"d1934697","state":"KNOWN"},"candidate_commit":{"text":"b5eaff9f","state":"KNOWN"}}}}`
	lDone      = `{"id":"e7","time":"2026-09-19T15:48:30Z","session_id":"s1","task_id":"task-1","kind":"workflow.completed","summary":"done","payload":{}}`
	lNoGraph   = `{"id":"e6","time":"2026-09-19T15:48:29Z","session_id":"s1","task_id":"task-1","kind":"run.receipt","summary":"receipt","payload":{"receipt":{"graph_digest":{"text":"d1934697","state":"UNKNOWN"}}}}`
	lAuthority = `{"id":"e8","time":"2026-09-19T15:47:30Z","session_id":"s1","task_id":"task-1","kind":"authority.resolved","summary":"","payload":{"question":"allow test edit?","option_id":"approve","option_label":"Approve once"}}`
)

func writeSession(t *testing.T, root, name string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustSession(t *testing.T, lines ...string) corpus.Record {
	t.Helper()
	r, err := Session(writeSession(t, t.TempDir(), "s1", lines...))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReviseThenAcceptEpisode(t *testing.T) {
	r := mustSession(t, lCreated, lReal, lPlan, lAuthority, lRevise, lAccept, lReceipt, lDone)
	if r.Objective == "" || r.ObjectiveID != "task-1" {
		t.Fatalf("objective not extracted: %+v", r)
	}
	if r.Outcome != corpus.OutcomeAccepted {
		t.Fatalf("outcome = %s, want ACCEPT", r.Outcome)
	}
	if len(r.PlanAttempts) != 1 || len(r.Reviews) != 2 || len(r.Rulings) != 1 {
		t.Fatalf("plans=%d reviews=%d rulings=%d", len(r.PlanAttempts), len(r.Reviews), len(r.Rulings))
	}
	if r.Rulings[0].Answer != "Approve once" {
		t.Fatalf("ruling answer = %q", r.Rulings[0].Answer)
	}
	g := r.GraphSnapshot
	if g.State != corpus.SnapshotReferenced || g.MarkerDigest != "d1934697" || g.BuildCommit != "05feaf64" || g.Provenance == nil {
		t.Fatalf("snapshot = %+v", g)
	}
	if r.Reviews[0].Provenance.Locator != "line 5" || r.Reviews[0].Provenance.Digest == "" {
		t.Fatalf("review provenance = %+v", r.Reviews[0].Provenance)
	}
}

// DESIGN.md §3/§9: with no finding lifecycle in the ledger, a later ACCEPT is
// recorded as an observation and must never promote the label.
func TestFindingsAreNeverPromoted(t *testing.T) {
	r := mustSession(t, lCreated, lPlan, lRevise, lAccept, lDone)
	f := r.Reviews[0].Findings[0]
	if f.LabelState != corpus.ReviewerObservation {
		t.Fatalf("label_state = %s, want %s", f.LabelState, corpus.ReviewerObservation)
	}
	if f.Correction != "add a witness test" || f.ProofGap != "no test names report.go" {
		t.Fatalf("correction/proof_gap not extracted: %+v", f)
	}
	if !f.FollowedByAccept {
		t.Fatal("followed_by_accept not recorded for a finding a later candidate's review accepted")
	}
}

// An accept of the same candidate digest is not evidence the finding led to a change.
func TestAcceptOfSameCandidateIsNotFollowedByAccept(t *testing.T) {
	same := strings.Replace(lAccept, `"a698"`, `"d027"`, 1)
	r := mustSession(t, lCreated, lRevise, same, lDone)
	if r.Reviews[0].Findings[0].FollowedByAccept {
		t.Fatal("followed_by_accept set though the accepted candidate is the one that was revised")
	}
}

// DESIGN.md §2: AsOf must remove everything that became known after t.
func TestAsOfExcludesLaterFacts(t *testing.T) {
	r := mustSession(t, lCreated, lPlan, lAuthority, lRevise, lAccept, lReceipt, lDone)
	at := time.Date(2026, 9, 19, 15, 48, 0, 0, time.UTC) // after the revise, before the accept
	v := r.AsOf(at)
	if len(v.Reviews) != 1 || v.Reviews[0].Decision != "revise" {
		t.Fatalf("AsOf reviews = %+v, want only the revise", v.Reviews)
	}
	if v.Reviews[0].Findings[0].FollowedByAccept {
		t.Fatal("AsOf leaked followed_by_accept, which is only known after the later accept")
	}
	if v.Outcome != corpus.OutcomeOpen || v.Terminal != "" || v.CandidateCommit != "" {
		t.Fatalf("AsOf leaked end-of-episode facts: outcome=%s terminal=%q candidate=%q", v.Outcome, v.Terminal, v.CandidateCommit)
	}
	if !r.Reviews[0].Findings[0].FollowedByAccept {
		t.Fatal("AsOf mutated the full record")
	}
	early := r.AsOf(time.Date(2026, 9, 19, 15, 47, 0, 0, time.UTC))
	if len(early.PlanAttempts) != 0 || len(early.Rulings) != 0 {
		t.Fatalf("AsOf kept a plan or ruling from after t: plans=%d rulings=%d", len(early.PlanAttempts), len(early.Rulings))
	}
}

func TestCompletedWithoutAcceptingReviewIsNotAccepted(t *testing.T) {
	r := mustSession(t, lCreated, lPlan, lRevise, lDone)
	if r.Outcome != corpus.OutcomeOpen {
		t.Fatalf("outcome = %s, want OPEN for a completion no review accepted", r.Outcome)
	}
	r = mustSession(t, lCreated, lPlan, lDone)
	if r.Outcome != corpus.OutcomeOpen {
		t.Fatalf("outcome = %s, want OPEN for an unreviewed completion", r.Outcome)
	}
}

func TestUnknownReceiptGraphStaysUnreconstructable(t *testing.T) {
	r := mustSession(t, lCreated, lNoGraph)
	if r.GraphSnapshot.State != corpus.SnapshotUnreconstructable || r.GraphSnapshot.MarkerDigest != "" {
		t.Fatalf("snapshot = %+v, want UNRECONSTRUCTABLE with no digest", r.GraphSnapshot)
	}
}

func TestSyntheticRunIsClassified(t *testing.T) {
	r := mustSession(t, lCreated, lStub)
	if !r.Synthetic.Synthetic || r.Synthetic.Predicate != SyntheticPredicate {
		t.Fatalf("synthetic = %+v", r.Synthetic)
	}
	r = mustSession(t, lCreated, lReal)
	if r.Synthetic.Synthetic {
		t.Fatal("a run with a real agent was classified synthetic")
	}
}

func TestMalformedLineNamesTheLine(t *testing.T) {
	_, err := Session(writeSession(t, t.TempDir(), "s1", lCreated, `{"kind":`))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want an error naming line 2", err)
	}
}

func TestCensusListsEveryExcludedSubject(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "a-good", lCreated, lStub, lRevise, lAccept, lDone)
	writeSession(t, root, "a-real", lCreated, lReal, lRevise, lAccept, lDone)
	writeSession(t, root, "b-noobjective", lPlan)
	if err := os.MkdirAll(filepath.Join(root, "c-noledger"), 0o755); err != nil {
		t.Fatal(err)
	}
	var emitted int
	c, err := Walk(root, func(corpus.Record) error { emitted++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a-good": "", "a-real": "", "b-noobjective": ReasonNoObjective, "c-noledger": ReasonNoLedger}
	if len(c.Subjects) != len(want) {
		t.Fatalf("subjects = %+v", c.Subjects)
	}
	for _, s := range c.Subjects {
		if s.Reason != want[s.SessionDir] || s.Included != (want[s.SessionDir] == "") {
			t.Errorf("subject %s: included=%v reason=%q", s.SessionDir, s.Included, s.Reason)
		}
	}
	k := c.Counts
	if emitted != 2 || k.Included != 2 || k.Sessions != 4 || k.Synthetic != 1 ||
		k.ReviseThenAccept != 2 || k.ReviseThenAcceptReal != 1 || k.FindingLabels[corpus.ReviewerObservation] != 2 {
		t.Fatalf("emitted=%d counts=%+v", emitted, k)
	}
}

// DESIGN.md §1: sensei-prior reads sources and never writes them.
func TestWalkDoesNotModifySources(t *testing.T) {
	root := t.TempDir()
	path := writeSession(t, root, "s1", lCreated, lRevise, lAccept, lReceipt, lDone)
	before, beforeInfo := digest(t, path)
	if _, err := Walk(root, func(corpus.Record) error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, afterInfo := digest(t, path)
	if before != after || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("Walk modified a source ledger")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("Walk wrote into the source directory: %d entries", len(entries))
	}
}

func digest(t *testing.T, path string) ([32]byte, os.FileInfo) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b), info
}
