package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei-prior/internal/corpus"
)

func runLog(task, session string) string {
	return "task " + task + "  session " + session + "\n" +
		`{"id":"e","time":"2026-09-20T00:00:00Z","session_id":"` + session + `","task_id":"` + task + `","kind":"task.created","summary":"x","payload":null}` + "\n"
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileLabelsAreVerbatim(t *testing.T) {
	cases := map[string][2]string{ // file -> {objective label, DF label}
		"run-43b-resume1.jsonl":       {"43b", ""},
		"run-obj49-r1-resume1.jsonl":  {"49", ""},
		"run-02a.jsonl":               {"02a", ""},
		"run-35-df25-authority.jsonl": {"35", ""},
		"run-df16.jsonl":              {"", "df16"},
		"run-df10-resume.jsonl":       {"", "df10"},
		"run-continuity.jsonl":        {"", ""},
		"run-p195.jsonl":              {"", ""},
	}
	dir := t.TempDir()
	i := 0
	for name := range cases {
		i++
		write(t, dir, name, runLog("task-"+name, "session-"+name))
	}
	ix, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Runs) != len(cases) {
		t.Fatalf("runs = %d, want %d", len(ix.Runs), len(cases))
	}
	for _, r := range ix.Runs {
		want := cases[r.File]
		if r.ObjectiveLabel != want[0] || r.DFLabel != want[1] {
			t.Errorf("%s: objective=%q df=%q, want %q %q", r.File, r.ObjectiveLabel, r.DFLabel, want[0], want[1])
		}
	}
}

func TestIdentityLineForms(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "run-1-fresh.jsonl", runLog("task-1", "s-1"))
	write(t, dir, "run-1-resume1.jsonl", "resuming "+runLog("task-1", "s-1"))
	write(t, dir, "run-2-fresh.jsonl", "github bridge: banner\n  kept 3 review request(s)\n"+runLog("task-2", "s-2"))
	ix, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.ByTask["task-1"]) != 2 || len(ix.ByTask["task-2"]) != 1 {
		t.Fatalf("by task = %+v", ix.ByTask)
	}
	if loc := ix.ByTask["task-2"][0].Provenance.Locator; loc != "line 3" {
		t.Fatalf("identity locator = %q, want line 3", loc)
	}

}

func TestUnidentifiableRunsAreExcludedWithReasons(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "run-1-fresh.jsonl", runLog("task-1", "s-1"))
	write(t, dir, "run-2-fresh.jsonl", "")
	write(t, dir, "run-3-fresh.jsonl", `{"session_id":"s","task_id":"t"}`+"\n")
	write(t, dir, "run-4-fresh.jsonl", "just a banner\n")
	write(t, dir, "run-5-fresh.jsonl", "task task-A  session session-A\n"+`{"session_id":"session-A","task_id":"task-B"}`+"\n")
	// NUL padding after a crash: the identity is still readable.
	write(t, dir, "run-6-fresh.jsonl", runLog("task-6", "s-6")+strings.Repeat("\x00", 64))
	ix, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"run-2-fresh.jsonl": "no identity line",
		"run-3-fresh.jsonl": "before any identity line",
		"run-4-fresh.jsonl": "no identity line",
		"run-5-fresh.jsonl": "task-B",
	}
	if len(ix.Excluded) != len(want) {
		t.Fatalf("excluded = %+v", ix.Excluded)
	}
	for _, e := range ix.Excluded {
		if !strings.Contains(e.Reason, want[e.File]) {
			t.Errorf("%s: reason %q, want it to mention %q", e.File, e.Reason, want[e.File])
		}
		for _, r := range ix.Runs {
			if r.File == e.File {
				t.Errorf("%s is both excluded and a run", e.File)
			}
		}
	}
	if len(ix.ByTask["task-1"]) != 1 || len(ix.ByTask["task-6"]) != 1 || len(ix.ByTask["task-A"]) != 0 {
		t.Fatalf("by task = %+v", ix.ByTask)
	}
}

func TestHeaderAndEventMustNameTheSameTask(t *testing.T) {
	dir := t.TempDir()
	body := "task task-A  session session-A\n" +
		`{"session_id":"session-A","task_id":"task-B","kind":"task.created"}` + "\n"
	write(t, dir, "run-1-fresh.jsonl", body)
	ix, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Excluded) != 1 || !strings.Contains(ix.Excluded[0].Reason, "task-B") || len(ix.ByTask) != 0 {
		t.Fatalf("excluded=%+v byTask=%+v, want the contradiction excluded and no identity kept", ix.Excluded, ix.ByTask)
	}
}

func TestObjectiveNumberNeverChoosesBetweenSources(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "run-43-fresh.jsonl", runLog("task-1", "s-1"))
	write(t, dir, "run-43-resume1.jsonl", runLog("task-1", "s-1"))
	write(t, dir, "run-43b-resume1.jsonl", runLog("task-2", "s-2"))
	write(t, dir, "run-43-fresh-run2.jsonl", runLog("task-2", "s-2"))
	ix, err := Load(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := corpus.Provenance{Source: "ledger", Locator: "line 1"}

	if n := ObjectiveNumber(ix, "task-1", "LAW: x", p); n.State != corpus.IdentityKnown || n.Value != "43" || len(n.Claims) != 2 {
		t.Fatalf("agreeing files: %+v", n)
	}
	// "43" and "43b" are different labels; neither is chosen.
	if n := ObjectiveNumber(ix, "task-2", "", p); n.State != corpus.IdentityAmbiguous || n.Value != "" {
		t.Fatalf("disagreeing files: %+v, want AMBIGUOUS with no value", n)
	}
	if n := ObjectiveNumber(ix, "task-1", "OBJECTIVE 44 - DF-1:\nLAW: x", p); n.State != corpus.IdentityAmbiguous {
		t.Fatalf("text disagreeing with files: %+v, want AMBIGUOUS", n)
	}
	if n := ObjectiveNumber(ix, "task-1", "OBJECTIVE 43 - DF-1:", p); n.State != corpus.IdentityKnown || len(n.Claims) != 3 {
		t.Fatalf("text agreeing with files: %+v", n)
	}
	if n := ObjectiveNumber(ix, "task-none", "LAW: x", p); n.State != corpus.IdentityAbsent || len(n.Claims) != 0 {
		t.Fatalf("no source: %+v, want ABSENT", n)
	}
	if n := ObjectiveNumber(nil, "task-none", "OBJECTIVE 9 - X:", p); n.State != corpus.IdentityKnown || n.Value != "9" {
		t.Fatalf("text only, no archive: %+v", n)
	}
}

func TestEraBasisIsVerifiedAgainstTheArchive(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "STRATEGY.md", "# s\n\nStatus: adopted 2026-09-27 from the analysis\n")
	defs := []EraDef{{ID: "knf", EffectiveFrom: "2026-09-27", BasisFile: "STRATEGY.md", BasisText: "Status: adopted 2026-09-27"}}
	ix, err := Load(dir, defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Eras) != 1 || ix.Eras[0].Basis.Locator != "line 3" || ix.Eras[0].Basis.Digest == "" {
		t.Fatalf("eras = %+v", ix.Eras)
	}
	defs[0].BasisText = "Status: adopted 2026-09-28"
	if _, err := Load(dir, defs); err == nil {
		t.Fatal("an era whose basis text is absent from the archive was accepted")
	}
}

func TestEraOf(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	eras := []Era{
		{ID: "a", EffectiveFrom: d("2026-09-18T00:00:00Z")},
		{ID: "b", EffectiveFrom: d("2026-09-24T00:00:00Z")},
	}
	cases := []struct {
		at       string
		id       string
		boundary bool
	}{
		{"2026-09-10T00:00:00Z", PreArchiveEra, false},
		{"2026-09-20T12:00:00Z", "a", false},
		{"2026-09-23T20:00:00Z", "a", true}, // could be the 24th somewhere
		{"2026-09-24T03:00:00Z", "b", true}, // could still be the 23rd in ET
		{"2026-09-26T00:00:00Z", "b", false},
	}
	for _, c := range cases {
		m := EraOf(eras, d(c.at))
		if m.ID != c.id || m.BoundaryDay != c.boundary {
			t.Errorf("%s: era=%s boundary=%v, want %s %v", c.at, m.ID, m.BoundaryDay, c.id, c.boundary)
		}
	}
}
