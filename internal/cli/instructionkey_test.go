package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/review"
	"github.com/schuettc/galley/internal/unsent"
)

// THE ROUND HANDS OVER ITS KEYS, AND FOR THE WHOLE OF PHASE 3 IT DID NOT.
//
// The MCP contract tells the agent to name in its ack's `answers` "which
// instruction it answers BY THE KEY THE ROUND HANDED YOU". `instructionPayload`
// carried Text, Quote and At — no key — so every CLI surface that renders a
// round (`galley pending`, `galley wait --json`, the channel notification)
// handed over no key, and compliance was structurally impossible.
//
// Measured against a real review on 2026-08-22: the paired agent sent `quote`
// and `note` on all 23 changes across three landed rounds and `answers` on none
// of them. Every change card then rendered with no ask beside it, and the asks
// piled onto the round card instead — two symptoms of one absent field.
//
// Nothing failed loudly because a JSON decoder ignores what it is not asked
// for: the server has always sent `key` on `instructionView`, and this side
// simply did not decode it. These checks are the ones that would have said so.

// TestTheLiveWireCarriesTheKeyIntoTheCLI decodes the server's own instruction
// shape into the CLI's and requires the key to survive.
//
// It is written against the JSON rather than against the Go types on purpose:
// `instructionView` is unexported in internal/serve, and the wire between them
// IS the json, which is the thing that was silently dropping a field.
func TestTheLiveWireCarriesTheKeyIntoTheCLI(t *testing.T) {
	// Exactly what /_galley/pending puts on the wire for one instruction.
	const wire = `{
	  "key": "cm-0fe440429378b2b0",
	  "text": "too many short, punchy sentences like this",
	  "quote": "Cognito mints every token",
	  "at": "2026-08-22T15:02:17Z",
	  "run": "r-1",
	  "anchor": "block",
	  "anchorKey": "md-4",
	  "blockKind": "paragraph"
	}`

	var got instructionPayload
	if err := json.Unmarshal([]byte(wire), &got); err != nil {
		t.Fatalf("decoding the server's instruction shape: %v", err)
	}
	if got.Key != "cm-0fe440429378b2b0" {
		t.Errorf("key did not survive the wire: got %q, want %q — the agent has nothing to put in `answers`",
			got.Key, "cm-0fe440429378b2b0")
	}
	if got.Text == "" || got.Quote == "" {
		t.Errorf("the fields that always worked stopped working: %+v", got)
	}
}

// TestTheOfflinePathCarriesTheKeyToo holds the equivalence this repository
// promises everywhere else: a verb works the same whether or not `galley edit`
// is running. A key that only exists live is a key an agent cannot rely on.
func TestTheOfflinePathCarriesTheKeyToo(t *testing.T) {
	at := time.Date(2026, 8, 22, 15, 2, 17, 0, time.UTC)
	doc := writeDoc(t, t.TempDir(), "d.md", "# T\n\nHello.\n")
	// A whole-document comment has no mark in the file: pending.json is the
	// only place it is, and offline it is read from there.
	if err := unsent.Save(unsent.Path(doc), unsent.File{Comments: []unsent.Comment{{
		Key: "cd-dbdcfb4746a99476", Kind: unsent.KindDocument, Author: review.AuthorCourt, At: at,
		Text: "in general this is good, but too many places where we're vague",
	}}}); err != nil {
		t.Fatal(err)
	}

	view, err := offlinePending(doc)
	if err != nil {
		t.Fatal(err)
	}
	got := view.Instructions
	if len(got) != 1 {
		t.Fatalf("built %d instructions, want 1", len(got))
	}
	if got[0].Key != "cd-dbdcfb4746a99476" {
		t.Errorf("the offline path dropped the key: got %q — live and offline would disagree",
			got[0].Key)
	}
}

// TestTheRenderedRoundNamesTheKey reads what the AGENT ACTUALLY SEES.
//
// The two checks above pin the data; this one pins the handover. `formatInstructions`
// is the one human rendering of a captured round and both carriers reach the agent
// through it — so a key present in the struct and absent from this string is a key
// that never arrives.
func TestTheRenderedRoundNamesTheKey(t *testing.T) {
	rendered := formatInstructions([]instructionPayload{
		{Key: "cm-0fe440429378b2b0", Quote: "Cognito mints every token", Text: "too many short sentences"},
		// A whole-document instruction has no quote. Its key must still print,
		// and must not end up adrift between two instructions with nothing
		// saying which one it belongs to.
		{Key: "cd-dbdcfb4746a99476", Text: "be clearer about what we are building"},
	})

	for _, want := range []string{"cm-0fe440429378b2b0", "cd-dbdcfb4746a99476"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the rendered round does not name key %q:\n%s", want, rendered)
		}
	}
	// Labelled with the word the ack uses, so the agent knows where it goes.
	if !strings.Contains(rendered, "key ") {
		t.Errorf("the key is printed but not labelled `key` — an unlabelled token beside a quote reads as decoration:\n%s", rendered)
	}
	// The quoteless instruction's key must lead its own block, not trail the
	// previous one.
	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	var sawQuoteless bool
	for i, line := range lines {
		if strings.HasPrefix(line, "[key cd-dbdcfb4746a99476]") {
			sawQuoteless = true
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "  ") {
				t.Errorf("a quoteless instruction's key is not followed by its own text:\n%s", rendered)
			}
		}
	}
	if !sawQuoteless {
		t.Errorf("a quoteless (whole-document) instruction printed no key line:\n%s", rendered)
	}
}
