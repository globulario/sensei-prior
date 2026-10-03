// Package archive reads the authored layer of the sensei-code objectives
// archive (DESIGN.md §9): which run file belongs to which task, and the dated
// documents that mark each era. It opens files only for reading.
//
// Every fact here is observational. Labels are kept exactly as the archive
// writes them ("43b" is not "43"), and when two sources disagree the result is
// AMBIGUOUS, never a choice.
package archive

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/globulario/sensei-prior/internal/corpus"
)

// Run is one archived run log and the identity it records.
type Run struct {
	File           string            `json:"file"`
	TaskID         string            `json:"task_id"`
	SessionID      string            `json:"session_id"`
	ObjectiveLabel string            `json:"objective_label,omitempty"`
	DFLabel        string            `json:"df_label,omitempty"`
	Provenance     corpus.Provenance `json:"provenance"`
}

// The archive's file-naming convention, stated as the predicate it is.
var (
	// run-43-fresh, run-43b-resume1, run-obj49-r1-resume1, run-02a, run-obj37r-resume
	objectiveFileLabel = regexp.MustCompile(`^run-(?:obj)?(\d+[a-z]?)(?:-|\.jsonl$)`)
	// run-df16, run-df20b, run-df10-resume
	dfFileLabel = regexp.MustCompile(`^run-(df\d+[a-z]?)(?:-|\.jsonl$)`)
	// the identity line of a run log, before its first event:
	// "task <task-id>  session <session-id>", or "resuming task ..." on a resume.
	// Non-JSON preamble lines (e.g. a github bridge banner) may precede it.
	header = regexp.MustCompile(`^(?:resuming )?task (\S+)\s+session (\S+)\s*$`)
)

// ObjectiveFilePredicate and DFFilePredicate name the rules for provenance.
const (
	ObjectiveFilePredicate = "file name matches ^run-(?:obj)?(\\d+[a-z]?)(?:-|.jsonl$)"
	DFFilePredicate        = "file name matches ^run-(df\\d+[a-z]?)(?:-|.jsonl$)"
)

// Index is the archive's authored layer, keyed by task.
type Index struct {
	Dir      string
	Runs     []Run
	ByTask   map[string][]Run
	Eras     []Era
	Excluded []Excluded // run files that state no usable identity
}

// Excluded is a run file whose identity could not be read, and why. It is
// reported in the census, never skipped silently.
type Excluded struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Load reads the identity of every run-*.jsonl in dir and verifies each era's
// basis. A run file that is empty, has no identity line, or whose identity line
// and first event name different tasks is excluded with its reason; it never
// contributes an identity. A missing era basis is an error for the whole load.
func Load(dir string, eras []EraDef) (*Index, error) {
	ix := &Index{Dir: dir, ByTask: map[string][]Run{}}
	names, err := filepath.Glob(filepath.Join(dir, "run-*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	for _, path := range names {
		r, err := readRun(path)
		if err != nil {
			ix.Excluded = append(ix.Excluded, Excluded{File: filepath.Base(path), Reason: err.Error()})
			continue
		}
		ix.Runs = append(ix.Runs, r)
		ix.ByTask[r.TaskID] = append(ix.ByTask[r.TaskID], r)
	}
	ix.Eras, err = verifyEras(dir, eras)
	if err != nil {
		return nil, err
	}
	return ix, nil
}

func readRun(path string) (Run, error) {
	f, err := os.Open(path)
	if err != nil {
		return Run{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var r Run
	line := 0
	for r.TaskID == "" && sc.Scan() {
		line++
		text := sc.Text()
		if strings.HasPrefix(text, "{") {
			return Run{}, fmt.Errorf("%s line %d: event before any identity line", path, line)
		}
		m := header.FindStringSubmatch(text)
		if m == nil {
			continue // preamble
		}
		sum := sha256.Sum256([]byte(text))
		r = Run{
			File: filepath.Base(path), TaskID: m[1], SessionID: m[2],
			Provenance: corpus.Provenance{Source: path, Locator: fmt.Sprintf("line %d", line), Digest: hex.EncodeToString(sum[:])},
		}
	}
	if r.TaskID == "" {
		if err := sc.Err(); err != nil {
			return Run{}, fmt.Errorf("%s: %w", path, err)
		}
		return Run{}, fmt.Errorf("%s: no identity line", path)
	}
	for sc.Scan() {
		var e struct {
			TaskID    string `json:"task_id"`
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.TaskID == "" {
			continue
		}
		if e.TaskID != r.TaskID || e.SessionID != r.SessionID {
			return Run{}, fmt.Errorf("%s: header names %s/%s but the first event names %s/%s",
				path, r.TaskID, r.SessionID, e.TaskID, e.SessionID)
		}
		break
	}
	if err := sc.Err(); err != nil {
		return Run{}, fmt.Errorf("%s: %w", path, err)
	}
	if m := objectiveFileLabel.FindStringSubmatch(r.File); m != nil {
		r.ObjectiveLabel = m[1]
	}
	if m := dfFileLabel.FindStringSubmatch(r.File); m != nil {
		r.DFLabel = m[1]
	}
	return r, nil
}

// objectiveTextLabel is an objective stating its own number on a line of its
// text, e.g. "OBJECTIVE 67 - DF-39:".
var objectiveTextLabel = regexp.MustCompile(`(?m)^OBJECTIVE (\d+[a-z]?)\b`)

const ObjectiveTextPredicate = "objective text has a line matching ^OBJECTIVE (\\d+[a-z]?)\\b"

// ObjectiveNumber resolves a task's objective label from every source that
// states one: the archive file names for that task, and the objective text.
// Values are compared verbatim. Disagreement is AMBIGUOUS; no source is ABSENT.
func ObjectiveNumber(ix *Index, taskID, objectiveText string, textProv corpus.Provenance) corpus.ObjectiveNumber {
	var claims []corpus.LabelClaim
	if ix != nil {
		for _, r := range ix.ByTask[taskID] {
			if r.ObjectiveLabel != "" {
				p := r.Provenance
				p.Locator = "file name " + r.File
				claims = append(claims, corpus.LabelClaim{Value: r.ObjectiveLabel, Predicate: ObjectiveFilePredicate, Provenance: p})
			}
		}
	}
	for _, m := range objectiveTextLabel.FindAllStringSubmatch(objectiveText, -1) {
		claims = append(claims, corpus.LabelClaim{Value: m[1], Predicate: ObjectiveTextPredicate, Provenance: textProv})
	}
	out := corpus.ObjectiveNumber{State: corpus.IdentityAbsent, Claims: claims}
	if len(claims) == 0 {
		out.Claims = []corpus.LabelClaim{}
		return out
	}
	for _, c := range claims {
		if c.Value != claims[0].Value {
			out.State = corpus.IdentityAmbiguous
			return out
		}
	}
	out.State = corpus.IdentityKnown
	out.Value = claims[0].Value
	return out
}

// RunFiles lists the archive files recorded for a task.
func RunFiles(ix *Index, taskID string) []string {
	if ix == nil {
		return nil
	}
	var out []string
	for _, r := range ix.ByTask[taskID] {
		out = append(out, r.File)
	}
	return out
}

// DFLabels lists the DF labels the archive file names give a task, verbatim.
func DFLabels(ix *Index, taskID string) []string {
	if ix == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range ix.ByTask[taskID] {
		if r.DFLabel != "" && !seen[r.DFLabel] {
			seen[r.DFLabel] = true
			out = append(out, r.DFLabel)
		}
	}
	return out
}

func lineOf(text, needle string) (int, string, bool) {
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, needle) {
			return i + 1, l, true
		}
	}
	return 0, "", false
}
