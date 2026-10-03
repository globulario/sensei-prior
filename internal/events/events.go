// Package events reads sensei-code session ledgers (events.jsonl) read-only.
//
// It mirrors sensei-code's event envelope (internal/event/event.go) rather than
// importing it: sensei-code's packages are internal, and sensei-prior must not
// be able to reach any writer.
package events

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Event is one line of events.jsonl.
type Event struct {
	ID        string          `json:"id"`
	Time      time.Time       `json:"time"`
	SessionID string          `json:"session_id"`
	TaskID    string          `json:"task_id"`
	Source    string          `json:"source"`
	Kind      string          `json:"kind"`
	Summary   string          `json:"summary"`
	Payload   json.RawMessage `json:"payload"`

	// Line is the 1-based line number and Digest the sha256 of the raw line.
	// They are the provenance locator for anything read from this event.
	Line   int    `json:"-"`
	Digest string `json:"-"`
}

// maxLine bounds one event. sensei-code caps events at write time; this is a
// reader bound, generous enough for any plan payload seen so far.
const maxLine = 64 << 20

// Load reads every event in path. A malformed line is an error that names the
// line: a partial ledger read as a whole one would silently drop history.
func Load(path string) ([]Event, error) {
	f, err := os.Open(path) // read-only by construction
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), maxLine)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		sum := sha256.Sum256(raw)
		e.Line = line
		e.Digest = hex.EncodeToString(sum[:])
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s after line %d: %w", path, line, err)
	}
	return out, nil
}
