package extract

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/globulario/sensei-prior/internal/archive"
	"github.com/globulario/sensei-prior/internal/corpus"
)

func TestStructureRecognizesSectionsByMembership(t *testing.T) {
	text := "OBJECTIVE 67 - DF-39:\n" +
		"LAW: scope is compared under one attempt.\n" +
		"LAWYER: not a law.\n" +
		"MEASURED SPECIMEN:\nobjective 61 escaped.\n" +
		"W1 EXTRA EXISTING PRODUCTION FILE:\nrefused.\n" +
		"FORBIDDEN REPAIRS:\ndo not widen the scope.\n" +
		"ARCHITECT:\nplan it.\n" +
		"RULING-112 PLAN-SCOPE COMPLETENESS:\nnamed files only.\n"
	s := Structure(text)
	var kinds []string
	for _, sec := range s.Sections {
		kinds = append(kinds, sec.Kind)
		if sec.Locator == "" || sec.Digest == "" {
			t.Errorf("section %s has no locator or digest", sec.Kind)
		}
	}
	want := []string{"LAW", "MEASURED_SPECIMEN", "WITNESS_ITEM", "FORBIDDEN_REPAIRS", "RULING_AMENDMENT"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	wantUnrec := []string{"OBJECTIVE 67 - DF-39:", "LAWYER:", "ARCHITECT:"}
	if !reflect.DeepEqual(s.Unrecognized, wantUnrec) {
		t.Fatalf("unrecognized = %v, want %v", s.Unrecognized, wantUnrec)
	}
}

// A section's digest covers its body, so an edited body changes it.
func TestSectionDigestCoversTheBody(t *testing.T) {
	a := Structure("LAW: x\nbody one\nSCOPE:\ns")
	b := Structure("LAW: x\nbody two\nSCOPE:\ns")
	if a.Sections[0].Digest == b.Sections[0].Digest {
		t.Fatal("LAW digest unchanged when its body changed")
	}
	if a.Sections[1].Digest != b.Sections[1].Digest {
		t.Fatal("SCOPE digest changed though its body did not")
	}
}

func TestRulingReferencesCarryNoInterpretation(t *testing.T) {
	at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	rec := corpus.Record{AvailableAt: at, Provenance: []corpus.Provenance{{Source: "ledger", Locator: "line 1"}}}
	refs := RulingReferences("per ruling 85, see\nRULING-112 PLAN-SCOPE:\nand Ruling 85 again; rulings in general", rec)
	if len(refs) != 2 || refs[0].Ruling != 85 || refs[0].Mentions != 2 || refs[1].Ruling != 112 || refs[1].Form != "RULING-112" {
		t.Fatalf("refs = %+v", refs)
	}
	for _, r := range refs {
		if !r.AvailableAt.Equal(at) || r.DescribesTime != nil {
			t.Errorf("ruling %d: available_at=%v describes_time=%v; the text dates neither the ruling nor more than itself", r.Ruling, r.AvailableAt, r.DescribesTime)
		}
	}
	if refs[0].Provenance.Locator != "line 1, objective line 1" || refs[1].Provenance.Locator != "line 1, objective line 2" {
		t.Fatalf("locators = %q %q", refs[0].Provenance.Locator, refs[1].Provenance.Locator)
	}
}

// End to end over the fabricated fixtures in testdata/synthetic.
func TestWalkWithSyntheticArchive(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "synthetic")
	eras := []archive.EraDef{{ID: "synthetic-era", EffectiveFrom: "2026-01-05", BasisFile: "STRATEGY.md", BasisText: "Status: adopted 2026-01-05"}}
	ix, err := archive.Load(filepath.Join(root, "archive"), eras)
	if err != nil {
		t.Fatal(err)
	}
	var recs []corpus.Record
	c, err := Walk(filepath.Join(root, "sessions"), ix, func(r corpus.Record) error { recs = append(recs, r); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	r := recs[0]
	if r.ObjectiveNumber.State != corpus.IdentityKnown || r.ObjectiveNumber.Value != "7" || len(r.ObjectiveNumber.Claims) != 2 {
		t.Fatalf("objective number = %+v", r.ObjectiveNumber)
	}
	if r.Era == nil || r.Era.ID != "synthetic-era" || r.Era.Basis == nil {
		t.Fatalf("era = %+v", r.Era)
	}
	if !reflect.DeepEqual(c.ArchiveRunsWithoutLedger, []string{"run-obj8-r1-fresh.jsonl"}) {
		t.Fatalf("archive runs without ledger = %v", c.ArchiveRunsWithoutLedger)
	}
	k := c.Counts
	if k.SectionRecords["LAW"] != 1 || k.SectionRecords["WITNESS_ITEM"] != 1 || k.DistinctRulings != 1 ||
		k.FindingsCorrection != 1 || k.FindingsProofGap != 1 || k.Eras["synthetic-era"] != 1 ||
		k.ObjectiveNumbers[corpus.IdentityKnown] != 1 || k.UnrecognizedHeaders["OBJECTIVE 7 - SYNTHETIC:"] != 1 {
		t.Fatalf("counts = %+v", k)
	}
}
