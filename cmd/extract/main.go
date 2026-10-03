// Command extract builds the Stage 0 corpus from sensei-code session ledgers.
//
//	extract -sessions <repo>/.sensei-code/sessions [-archive <objectives archive>] -out out/
//
// It writes out/corpus.jsonl (one record per line) and out/census.json. The
// sources are only opened for reading. Keep -out outside any committed path:
// the corpus quotes objective text and rulings (DESIGN.md §9).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei-prior/internal/archive"
	"github.com/globulario/sensei-prior/internal/corpus"
	"github.com/globulario/sensei-prior/internal/extract"
)

func main() {
	sessions := flag.String("sessions", "", "sensei-code sessions directory (required)")
	arch := flag.String("archive", "", "sensei-code objectives archive (optional; enables objective numbers and eras)")
	out := flag.String("out", "out", "output directory")
	flag.Parse()
	if *sessions == "" {
		fmt.Fprintln(os.Stderr, "extract: -sessions is required")
		os.Exit(2)
	}
	if err := run(*sessions, *arch, *out); err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		os.Exit(1)
	}
}

func run(sessions, arch, out string) error {
	var ix *archive.Index
	if arch != "" {
		var err error
		if ix, err = archive.Load(arch, archive.DefaultEras); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(out, "corpus.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)

	census, err := extract.Walk(sessions, ix, func(r corpus.Record) error { return enc.Encode(r) })
	if err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(census, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "census.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	k := census.Counts
	fmt.Printf("sessions %d  included %d  synthetic %d  revise→accept %d (real %d)  snapshot referenced %d\n",
		k.Sessions, k.Included, k.Synthetic, k.ReviseThenAccept, k.ReviseThenAcceptReal, k.SnapshotReferenced)
	for reason, n := range k.Excluded {
		fmt.Printf("  excluded %d: %s\n", n, reason)
	}
	fmt.Printf("findings %d  with correction %d  with proof_gap %d\n", k.Findings, k.FindingsCorrection, k.FindingsProofGap)
	fmt.Printf("objective numbers %v  eras %v (boundary day %d)\n", k.ObjectiveNumbers, k.Eras, k.BoundaryDay)
	fmt.Printf("records citing rulings %d  distinct rulings %d  archive runs without ledger %d  archive runs excluded %d\n",
		k.RecordsCitingRulings, k.DistinctRulings, len(census.ArchiveRunsWithoutLedger), len(census.ArchiveRunsExcluded))
	return nil
}
