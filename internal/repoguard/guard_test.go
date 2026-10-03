// Package repoguard enforces DESIGN.md §10: no historical corpus is a
// repository artifact. The only data a commit may carry is fabricated fixture
// material under testdata/synthetic/.
package repoguard

import (
	"os/exec"
	"path"
	"strings"
	"testing"
)

// forbidden are tracked-path shapes that hold extracted or derived corpus
// material. Matching is on the slash-separated path git reports.
var forbidden = []struct {
	name  string
	match func(p string) bool
}{
	{"out/ directory", func(p string) bool { return hasDir(p, "out") }},
	{"corpus/ directory", func(p string) bool { return hasDir(p, "corpus") }},
	{"snapshots/ directory", func(p string) bool { return hasDir(p, "snapshots") }},
	{"training/ directory", func(p string) bool { return hasDir(p, "training") }},
	{"corpus.jsonl", func(p string) bool { return path.Base(p) == "corpus.jsonl" }},
	{"census.json", func(p string) bool { return path.Base(p) == "census.json" }},
	{"*.corpus.jsonl", func(p string) bool { return strings.HasSuffix(p, ".corpus.jsonl") }},
	{"*.episode.jsonl", func(p string) bool { return strings.HasSuffix(p, ".episode.jsonl") }},
	{"*.checkpoint", func(p string) bool { return strings.HasSuffix(p, ".checkpoint") }},
	{"events.jsonl outside synthetic fixtures", func(p string) bool { return path.Base(p) == "events.jsonl" }},
	{"run-*.jsonl outside synthetic fixtures", func(p string) bool {
		b := path.Base(p)
		return strings.HasPrefix(b, "run-") && strings.HasSuffix(b, ".jsonl")
	}},
}

// hasDir reports whether p lies under the top-level directory dir. Only the
// repository root is reserved: internal/corpus is source code, not a corpus.
func hasDir(p, dir string) bool { return strings.HasPrefix(p, dir+"/") }

func synthetic(p string) bool { return strings.HasPrefix(p, "testdata/synthetic/") }

func violations(paths []string) []string {
	var out []string
	for _, p := range paths {
		if synthetic(p) {
			continue
		}
		for _, f := range forbidden {
			if f.match(p) {
				out = append(out, p+" ("+f.name+")")
				break
			}
		}
	}
	return out
}

func TestNoCorpusIsTracked(t *testing.T) {
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v (this guard needs a git checkout; it does not skip)", err)
	}
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = strings.TrimSpace(string(top))
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	paths := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	if len(paths) < 5 {
		t.Fatalf("git ls-files returned %d paths; the guard is not seeing the repository", len(paths))
	}
	if v := violations(paths); len(v) > 0 {
		t.Fatalf("corpus material is tracked or staged for tracking (DESIGN.md §10):\n  %s", strings.Join(v, "\n  "))
	}
}

// The guard must fire on each shape it names, and spare synthetic fixtures.
func TestGuardDetectsEachForbiddenShape(t *testing.T) {
	bad := []string{
		"out/corpus.jsonl", "corpus/x.json", "snapshots/g.nt", "training/split.json",
		"corpus.jsonl", "census.json", "a.corpus.jsonl", "b.episode.jsonl", "m.checkpoint",
		"sessions/s/events.jsonl", "archive/run-obj7-r1-fresh.jsonl",
		// only testdata/synthetic/ is exempt, not testdata/ as a whole
		"testdata/real/events.jsonl", "internal/extract/testdata/run-obj7-r1-fresh.jsonl",
	}
	for _, p := range bad {
		if len(violations([]string{p})) != 1 {
			t.Errorf("guard missed %s", p)
		}
	}
	good := []string{
		"testdata/synthetic/sessions/session-synthetic-1/events.jsonl",
		"testdata/synthetic/archive/run-obj7-r1-fresh.jsonl",
		"internal/extract/census.go", "internal/corpus/record.go", "DESIGN.md", "cmd/extract/main.go",
	}
	if v := violations(good); len(v) != 0 {
		t.Errorf("guard flagged allowed paths: %v", v)
	}
}
