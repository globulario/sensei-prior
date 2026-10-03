package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei-prior/internal/archive"
	"github.com/globulario/sensei-prior/internal/corpus"
)

// sectionKinds is the closed vocabulary of objective sections. A header is
// recognized only by membership; anything else that looks like a header is
// listed as unrecognized, so a gap in the vocabulary is visible in the census
// instead of silently absent. Order matters only for readability.
var sectionKinds = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"LAW", regexp.MustCompile(`^LAW(?: \d+| \([^)]*\))?:`)},
	{"MEASURED_SPECIMEN", regexp.MustCompile(`^MEASURED SPECIMEN:`)},
	{"MEASUREMENT", regexp.MustCompile(`^MEASURED \d{4}-\d{2}-\d{2}`)},
	{"REQUIRED_BEHAVIOUR", regexp.MustCompile(`^REQUIRED BEHAVIOUR:`)},
	{"WITNESSES", regexp.MustCompile(`^(?:WITNESSES|WITNESS REQUIREMENT):`)},
	{"WITNESS_ITEM", regexp.MustCompile(`^W\d+ [A-Z0-9 -]+:`)},
	{"FORBIDDEN_REPAIRS", regexp.MustCompile(`^FORBIDDEN(?: REPAIRS)?:`)},
	{"NON_GOALS", regexp.MustCompile(`^NON-GOALS?:`)},
	{"SUCCESS_CONDITION", regexp.MustCompile(`^SUCCESS(?: CONDITION)?:`)},
	{"SCOPE", regexp.MustCompile(`^SCOPE:`)},
	{"KEY_INVARIANT", regexp.MustCompile(`^KEY INVARIANT:`)},
	{"RULING_NOTE", regexp.MustCompile(`^RULING NOTE:`)},
	{"RULING_AMENDMENT", regexp.MustCompile(`^RULING[- ]\d+\b[^:]*:`)},
}

var headerLike = regexp.MustCompile(`^[A-Z][A-Z0-9 /()-]{1,60}:`)

// Structure records which sections the objective text physically contains.
// It judges nothing about quality (DESIGN.md §3a).
func Structure(text string) corpus.ObjectiveStructure {
	lines := strings.Split(text, "\n")
	type hit struct {
		line   int
		kind   string // "" for unrecognized
		header string
	}
	var hits []hit
	for i, l := range lines {
		h := headerLike.FindString(l)
		if h == "" {
			continue
		}
		kind := ""
		for _, k := range sectionKinds {
			if k.re.MatchString(l) {
				kind = k.kind
				break
			}
		}
		hits = append(hits, hit{i, kind, h})
	}
	out := corpus.ObjectiveStructure{Sections: []corpus.Section{}, Unrecognized: []string{}}
	for n, h := range hits {
		end := len(lines)
		if n+1 < len(hits) {
			end = hits[n+1].line
		}
		if h.kind == "" {
			out.Unrecognized = append(out.Unrecognized, h.header)
			continue
		}
		sum := sha256.Sum256([]byte(strings.Join(lines[h.line:end], "\n")))
		out.Sections = append(out.Sections, corpus.Section{
			Kind: h.kind, Header: h.header,
			Locator: "objective line " + strconv.Itoa(h.line+1), Digest: hex.EncodeToString(sum[:]),
		})
	}
	return out
}

var rulingRef = regexp.MustCompile(`(?i)\bruling[ -]#?(\d+)\b`)

// RulingPredicate is the rule that finds a ruling reference.
const RulingPredicate = `objective text matches (?i)\bruling[ -]#?(\d+)\b`

// RulingReferences lists the rulings the objective text cites, one entry per
// ruling number with its first locator. It records the reference only: what a
// ruling means, or which matrix cell it belongs to, is not derived here.
// DescribesTime is left unset because the text does not say when the ruling
// was made; AvailableAt is when the objective text existed.
func RulingReferences(text string, rec corpus.Record) []corpus.RulingReference {
	first := map[int]corpus.RulingReference{}
	for i, l := range strings.Split(text, "\n") {
		for _, m := range rulingRef.FindAllStringSubmatch(l, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if r, ok := first[n]; ok {
				r.Mentions++
				first[n] = r
				continue
			}
			p := corpus.Provenance{}
			if len(rec.Provenance) > 0 {
				p = rec.Provenance[0]
			}
			p.Locator += ", objective line " + strconv.Itoa(i+1)
			first[n] = corpus.RulingReference{
				Ruling: n, Form: m[0], Mentions: 1, Predicate: RulingPredicate,
				AvailableAt: rec.AvailableAt, Provenance: p,
			}
		}
	}
	out := make([]corpus.RulingReference, 0, len(first))
	for _, r := range first {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ruling < out[j].Ruling })
	return out
}

// Enrich adds the Stage 0b authored layer to a record. Structure and ruling
// references come from the objective text, which existed when the task was
// created; the objective number, DF labels and era need the archive.
func Enrich(r *corpus.Record, ix *archive.Index) {
	textProv := corpus.Provenance{}
	if len(r.Provenance) > 0 {
		textProv = r.Provenance[0]
	}
	r.ObjectiveStructure = Structure(r.Objective)
	r.RulingReferences = RulingReferences(r.Objective, *r)
	r.ObjectiveNumber = archive.ObjectiveNumber(ix, r.ObjectiveID, r.Objective, textProv)
	r.DFLabels = archive.DFLabels(ix, r.ObjectiveID)
	r.ArchiveRuns = archive.RunFiles(ix, r.ObjectiveID)
	if r.DFLabels == nil {
		r.DFLabels = []string{}
	}
	if r.ArchiveRuns == nil {
		r.ArchiveRuns = []string{}
	}
	if ix != nil {
		m := archive.EraOf(ix.Eras, r.AvailableAt)
		r.Era = &m
	}
}
