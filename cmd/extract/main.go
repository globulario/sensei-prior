// Command extract builds the Stage 0 corpus from sensei-code session ledgers.
//
//	extract -sessions <repo>/.sensei-code/sessions -out out/
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

	"github.com/globulario/sensei-prior/internal/corpus"
	"github.com/globulario/sensei-prior/internal/extract"
)

func main() {
	sessions := flag.String("sessions", "", "sensei-code sessions directory (required)")
	out := flag.String("out", "out", "output directory")
	flag.Parse()
	if *sessions == "" {
		fmt.Fprintln(os.Stderr, "extract: -sessions is required")
		os.Exit(2)
	}
	if err := run(*sessions, *out); err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		os.Exit(1)
	}
}

func run(sessions, out string) error {
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

	census, err := extract.Walk(sessions, func(r corpus.Record) error { return enc.Encode(r) })
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
	return nil
}
