package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/globulario/sensei-prior/internal/corpus"
)

// EraDef is an authored claim that a constitution took effect on a date, with
// the exact archive line that dates it. The claim is checked against the
// archive on every load; it is never taken on trust.
type EraDef struct {
	ID            string
	EffectiveFrom string // YYYY-MM-DD, as the basis line writes it
	BasisFile     string // relative to the archive directory
	BasisText     string // must occur verbatim on one line of BasisFile
}

// DefaultEras are the dated strategy changes the archive records. Era is
// historical context, not quality (DESIGN.md §3a): an episode from an earlier
// era is true history under the constitution of its day.
var DefaultEras = []EraDef{
	{ID: "campaign-2026-09-18", EffectiveFrom: "2026-09-18", BasisFile: "00-README.md",
		BasisText: "# Self-improvement objectives — 2026-09-18"},
	{ID: "pattern-2026-09-24", EffectiveFrom: "2026-09-24", BasisFile: "PATTERN.md",
		BasisText: "Status: derived 2026-09-24"},
	{ID: "knf-2026-09-27", EffectiveFrom: "2026-09-27", BasisFile: "STRATEGY.md",
		BasisText: "Status: adopted 2026-09-27"},
}

// PreArchiveEra is the membership of an episode older than every era.
const PreArchiveEra = "before-archive"

type Era struct {
	ID            string            `json:"id"`
	EffectiveFrom time.Time         `json:"effective_from"`
	Basis         corpus.Provenance `json:"basis"`
}

func verifyEras(dir string, defs []EraDef) ([]Era, error) {
	var out []Era
	var prev time.Time
	for _, d := range defs {
		from, err := time.Parse("2006-01-02", d.EffectiveFrom)
		if err != nil {
			return nil, fmt.Errorf("era %s: %w", d.ID, err)
		}
		if !from.After(prev) {
			return nil, fmt.Errorf("era %s: effective dates must strictly increase", d.ID)
		}
		prev = from
		path := filepath.Join(dir, d.BasisFile)
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("era %s basis: %w", d.ID, err)
		}
		n, line, ok := lineOf(string(b), d.BasisText)
		if !ok {
			return nil, fmt.Errorf("era %s: basis text %q no longer occurs in %s", d.ID, d.BasisText, path)
		}
		sum := sha256.Sum256([]byte(line))
		out = append(out, Era{ID: d.ID, EffectiveFrom: from, Basis: corpus.Provenance{
			Source: path, Locator: fmt.Sprintf("line %d", n), Digest: hex.EncodeToString(sum[:]),
		}})
	}
	return out, nil
}

// EraOf places t in an era. The basis dates carry no time zone, so an episode
// within a day either side of a boundary is marked BoundaryDay rather than
// given a precision the source does not have.
func EraOf(eras []Era, t time.Time) corpus.EraMembership {
	m := corpus.EraMembership{ID: PreArchiveEra}
	for _, e := range eras {
		if d := t.Sub(e.EffectiveFrom); d > -24*time.Hour && d < 24*time.Hour {
			m.BoundaryDay = true
		}
		if !t.Before(e.EffectiveFrom) {
			m.ID = e.ID
			b := e.Basis
			m.Basis = &b
		}
	}
	return m
}
