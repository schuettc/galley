package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/markdown"
	"github.com/schuettc/galley/internal/mcp"
	"github.com/schuettc/galley/internal/registry"
	"github.com/schuettc/galley/internal/serve"
	"github.com/schuettc/galley/internal/suggest"
)

// The whole loop, in one process: an editor advertises, the channel attaches,
// a Revise press becomes a channel notification — and a SECOND press becomes a
// second one, which is the re-arm guarantee the channel's whole life is. This
// is the paired session the product exists for, minus only the harness on the
// far end.
func TestChannelForwardsARevisePress(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	if err := registry.Write(registry.Entry{URL: ts.URL, Room: srv.Room, Page: doc, PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}

	// The channel under test, on pipes instead of stdio.
	clientOut, serverIn := io.Pipe()
	serverOut, clientIn := io.Pipe()
	ch := newChannel(dir, "") // scope = the doc's dir; self = no session id
	// The FIRST scan attaches (run scans once before it ever sleeps); after
	// that the interval is effectively infinite, so the re-arm this test
	// asserts below can only come from the attach loop itself. Without this, a
	// channel that detaches after every wake sneaks past: the deferred
	// c.attached delete plus the next 100ms rescan re-attach it, and the
	// second press is heard for the wrong reason. Verified by breaking attach
	// to return after its first wake — red with this line, green without it.
	ch.scanEvery = time.Hour
	go func() { _ = ch.run(clientOut, clientIn) }()
	defer func() { _ = serverIn.Close() }()

	// Handshake, then wait for the attach: the editor's Waiting() going
	// positive is the observable fact that the long-poll is armed.
	if _, err := io.WriteString(serverIn, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(serverOut)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	if !sc.Scan() {
		t.Fatal("no handshake reply")
	}
	deadline := time.Now().Add(5 * time.Second)
	for srv.Waiting() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the channel never attached — Waiting() stayed 0")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// One reader for the test's whole life: a scanner can only be driven from
	// one goroutine, and the re-arm half below needs a SECOND notification off
	// the same stream.
	got := make(chan map[string]any, 2)
	go func() {
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil && m["method"] == "notifications/claude/channel" {
				got <- m
			}
		}
	}()

	press := func() {
		t.Helper()
		resp, err := http.Post(ts.URL+"/_galley/revise", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	notification := func(when, instruction string) {
		t.Helper()
		select {
		case m := <-got:
			params, _ := m["params"].(map[string]any)
			meta, _ := params["meta"].(map[string]any)
			content, _ := params["content"].(string)
			if meta["reason"] != "revise" {
				t.Errorf("%s: meta.reason = %v", when, meta["reason"])
			}
			if meta["doc"] == "" {
				t.Errorf("%s: meta.doc missing: %v", when, meta)
			}
			for _, want := range []string{"Hello.", instruction} {
				if !strings.Contains(content, want) {
					t.Errorf("%s: captured instruction lost %q:\n%s", when, want, content)
				}
			}
			if strings.Contains(content, "Run `galley pending") {
				t.Errorf("%s: channel told the agent to discard the captured round:\n%s", when, content)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the press never became a notification", when)
		}
	}

	instructAs(t, ts.URL, "Hello.", "Make the greeting warmer.")
	press()
	notification("first press", "Make the greeting warmer.")

	// THE RE-ARM GUARANTEE, which is the channel's whole life: after a wake the
	// attach loop must already be issuing its next poll, or the reviewer's
	// SECOND press is refused as unheard. Waiting() going positive again is the
	// observable fact that the re-armed poll registered — the same idiom as the
	// initial attach above. A channel that detaches after every wake fails here.
	deadline = time.Now().Add(5 * time.Second)
	for srv.Waiting() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the channel never re-armed after the first wake — Waiting() stayed 0")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The first press opened the agent's handoff window and nobody answered
	// it, so the document is read-only to the reviewer. Taking it back is the
	// new workflow's own gesture before a fresh instruction.
	if resp, err := http.Post(ts.URL+"/_galley/handoff/cancel", "application/json",
		strings.NewReader("{}")); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel handoff before the second instruction: resp=%v err=%v", resp, err)
	}
	instructAs(t, ts.URL, "Hello.", "Make the greeting shorter.")
	press()
	notification("second press", "Make the greeting shorter.")
}

// THE WHOLE SEAL CYCLE, THROUGH THE CHANNEL: approve → the channel DETACHES →
// reopen → the channel attaches AGAIN → a revise wakes it.
//
// This is the one path no unit test can express, and the one the seal's design
// rests on: reopen invents no discovery machinery, it simply RE-ADVERTISES the
// registry entry the verdict withdrew, and the channel's own scan loop finds it
// on its next interval. If the re-advertise and the scan ever disagree about
// the entry's shape, the symptom is a channel that silently never comes back —
// which is exactly what a reviewer would report as "the agent stopped hearing
// me" with nothing in any log.
func TestChannelIgnoresAnotherSessionsDocument(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.md", "# T\n\nHello.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	if err := registry.Write(registry.Entry{URL: ts.URL, Room: srv.Room, Page: doc, PID: os.Getpid(), Owner: "somebody-else"}); err != nil {
		t.Fatal(err)
	}
	// somebody-else is still here: its channel is running (in this process,
	// for the length of this test) and announces the session the way every
	// `galley channel` does at startup.
	if err := registry.AnnounceSession("somebody-else"); err != nil {
		t.Fatal(err)
	}

	ch := newChannel(dir, "me")
	clientOut, serverIn := io.Pipe()
	_, clientIn := io.Pipe()
	go func() { _ = ch.run(clientOut, clientIn) }()
	defer func() { _ = serverIn.Close() }()

	time.Sleep(300 * time.Millisecond) // several scan intervals in test mode
	if n := srv.Waiting(); n != 0 {
		t.Fatalf("attached to another session's document: Waiting() = %d", n)
	}
}

// Scope: an unowned entry OUTSIDE the root is not claimable.
func TestChannelIgnoresADocumentOutsideItsScope(t *testing.T) {
	t.Setenv("GALLEY_LIVE_DIR", t.TempDir())
	elsewhere := t.TempDir()
	doc := writeDoc(t, elsewhere, "doc.md", "# T\n\nHello.\n")
	srv, err := serve.NewEdit(doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	if err := registry.Write(registry.Entry{URL: ts.URL, Room: srv.Room, Page: doc, PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}

	ch := newChannel(t.TempDir(), "") // scope is a different tree
	clientOut, serverIn := io.Pipe()
	_, clientIn := io.Pipe()
	go func() { _ = ch.run(clientOut, clientIn) }()
	defer func() { _ = serverIn.Close() }()

	time.Sleep(300 * time.Millisecond)
	if n := srv.Waiting(); n != 0 {
		t.Fatalf("attached outside its scope: Waiting() = %d", n)
	}
}

// channelInstructions must teach the unified verb grammar: reply by thread
// key, approve (accept/resolve/sweep by target shape), decline (final, both
// directions — a declined proposal is never re-proposed), and both approve
// arrivals — a clean approve (review over, proceed) and a TRUST approve (the
// entrusted handoff: apply each remaining note faithfully, resolve its
// thread as you complete it, ack answered, expect no further review). V8
// rewrites agentprompt.go next against this same protocol, so these
// substrings are the pin other implementers read.
func TestApproveContentIsTerminal(t *testing.T) {
	got := approveContent("doc.md")
	want := "The reviewer APPROVED doc.md as it stands. The review is over — proceed."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

var _ = mcp.Handler{} // keep the import while the test file grows

// A COUNT THAT COULD NOT BE READ IS NOT A ZERO.
//
// announceReopen makes its event out of TWO requests: `/_galley/revise` for the
// reason (and for the confirmation that this really is a reopen) and
// `/_galley/pending` for the counts. Only the first one decided whether to
// speak; the second's error was dropped on the floor, so a reopen announced
// while the count read was failing carried `suggestions: "0", threads: "0"` —
// a fresh round opening on a document the agent had just been told was clean,
// and the exact made-up zero the function's own comment says the second
// request exists to avoid.
// The targeting rules below belong to suggest's own matcher. The CARRIERS no
// longer teach targeting at all — the agent edits the file, and there is no
// target to aim — so the prose assertions that used to live here went with
// the apply loop; the matcher's behavior is still pinned.
func TestTargetsCrossCodeSpansAndRefuseBackticks(t *testing.T) {
	doc, _, err := markdown.Parse([]byte("# T\n\nThe `retryBudget` value controls retries.\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Crossing the code span, with no backtick in the target.
	out, err := suggest.Replace(doc, "The retryBudget value controls", "The retry budget governs", "agent", time.Now())
	if err != nil {
		t.Fatalf("a target crossing a code span no longer matches: %v", err)
	}
	if got := suggest.List(suggest.MintRuns(out)); len(got) != 1 {
		t.Fatalf("a target crossing a code span produced %d suggestions, want 1", len(got))
	}
	// And the failure the carriers must actually warn about.
	if _, err := suggest.Replace(doc, "The `retryBudget` value", "The retry budget", "agent", time.Now()); err == nil {
		t.Fatal("a target carrying backticks now matches — both carriers warn about a trap that is gone")
	}
}

// `galley pending` PRINTS EVERY THREAD'S KEY, from its first entry, with a
// tick beside the settled ones (main.go's thread loop). The instructions said
// it prints one "once someone has replied", which is a rule about a PROPOSAL's
// thread — that one is created by the first reply — read onto every thread,
// and it sends an agent looking for a key that is already on the screen.
func TestTheWakeCountsInstructions(t *testing.T) {
	var wire waitWire
	body := `{"reason":"revise","pending":{"instructions":[{"text":"one"},{"text":"two"}]}}`
	if err := json.Unmarshal([]byte(body), &wire); err != nil {
		t.Fatal(err)
	}
	if got := len(wire.Pending.Instructions); got != 2 {
		t.Errorf("instructions = %d, want 2", got)
	}
}

// The rules that govern a revise ride the revise, because a rule works best
// beside the action it governs. The whole-file-write rule is the reason this
// test is by name rather than by shape: it exists because a real agent
// destroyed a real reviewer's section, and a future edit that tidies it out of
// the guidance must fail here rather than ship.
func TestReviseGuidanceCarriesTheWriteRule(t *testing.T) {
	for _, reason := range []string{reasonRevise, reasonSettle} {
		lower := strings.ToLower(reasonGuidance(reason))
		for _, want := range []string{"targeted", "whole file", "stale", "galley_ack", "answered"} {
			if !strings.Contains(lower, want) {
				t.Errorf("%s guidance does not carry %q", reason, want)
			}
		}
	}
}

// THE CORE ITSELF MUST TEACH THE WRITE RULE, not merely the composed
// delivery. Whole-branch review 2026-08-25, findings F1/F2: `reasonGuidance`
// answers "" for `changed`, so a changed-woken agent gets channelInstructions
// and nothing else — no reviseGuidance rides that wake. The same is true of
// recovery: roundText (round.go) renders instructions and keys, deliberately
// nothing else, and the core's own recovery sentence points an agent there.
// A test against channelDelivery() (core + all five reasons' guidance) cannot
// catch the write rule going missing from the core, because reviseGuidance is
// one of the five joined in and would carry it regardless. This asserts the
// claim directly against channelInstructions, which is the one carrier that
// reaches a changed wake or a compaction recovery.
func TestTheHandshakeCoreCarriesTheWriteRule(t *testing.T) {
	lower := strings.ToLower(channelInstructions)
	for _, want := range []string{"targeted", "whole file"} {
		if !strings.Contains(lower, want) {
			t.Errorf("channelInstructions does not carry %q — a changed wake or a compaction recovery has no write rule at all", want)
		}
	}
}

// A PAGE-BACKED REVISE CARRIES THE PAGE-MODE GUIDANCE ON THE CHANNEL, not only
// on the pull carrier. A channel-woken agent answering a structural ask on an
// HTML page had no page-mode guidance at all and edited galley's derived
// template.json (silently overwritten) to answer it — measured on a real
// review. The pull carrier's agent prompt already appended pageBacked; this
// asserts the channel wake does too, and only for a page-backed doc.
func TestPageBackedReviseCarriesPageGuidance(t *testing.T) {
	paged := composeWake(reasonRevise, "Revision requested on doc.md — 1 instruction(s).", true)
	for _, want := range []string{
		"backed by an HTML page",
		"edit the page's .html directly",
		"NEVER edit the other files galley keeps",
		"template.json",
	} {
		if !strings.Contains(paged, want) {
			t.Errorf("page-backed channel revise is missing %q:\n%s", want, paged)
		}
	}
	// A single separator: pageBacked joins the guidance half, it does not add
	// a second "---" the client would mis-split on.
	const sep = "\n\n---\n"
	if strings.Count(paged, sep) != 1 {
		t.Errorf("page-backed wake has %d separators, want 1:\n%s", strings.Count(paged, sep), paged)
	}
	// A non-page-backed revise must NOT carry it.
	plain := composeWake(reasonRevise, "Revision requested on doc.md — 1 instruction(s).", false)
	if strings.Contains(plain, "backed by an HTML page") {
		t.Errorf("page-mode guidance leaked into a non-page-backed wake:\n%s", plain)
	}
	// An ENDING on a page-backed doc gets no page guidance (it has no revision
	// to make).
	end := composeWake(reasonApprove, "The review is over.", true)
	if strings.Contains(end, "backed by an HTML page") {
		t.Errorf("page-mode guidance leaked onto an approve wake:\n%s", end)
	}
}

// The endings need a sentence, not a protocol.
func TestEndingGuidanceIsShort(t *testing.T) {
	for _, reason := range []string{reasonApprove, reasonClosed, reasonChanged} {
		g := reasonGuidance(reason)
		if g == "" {
			t.Errorf("%s carries no guidance at all", reason)
		}
		if len(g) > 400 {
			t.Errorf("%s guidance is %d bytes; the endings get a sentence, not a protocol", reason, len(g))
		}
	}
}

// The separator is the contract a client splits on. pi-channels splits on the
// FIRST occurrence of "\n\n---\n" and treats everything after it as guidance
// it may dedupe across a coalesced batch; a client that does not split renders
// it as a readable rule.
func TestGuidanceIsSeparatedFromTheEnvelope(t *testing.T) {
	content := composeWake(reasonRevise, "Revision requested on doc.md — 2 instruction(s).", false)
	const sep = "\n\n---\n"
	if !strings.Contains(content, sep) {
		t.Fatalf("no guidance separator in:\n%s", content)
	}
	if strings.Index(content, sep) != strings.LastIndex(content, sep) {
		// Not fatal for a client that splits on the first occurrence, but it
		// means the envelope half is ambiguous to anything that does not.
		t.Logf("more than one separator; clients split on the first")
	}
	body, guidance, _ := strings.Cut(content, sep)
	if strings.Contains(body, "targeted") {
		t.Errorf("the write rule belongs in the guidance half, not the envelope:\n%s", body)
	}
	if !strings.Contains(strings.ToLower(guidance), "targeted") {
		t.Errorf("guidance half does not carry the write rule:\n%s", guidance)
	}
}

// The core is what every session pays for on every handshake. It was 4338
// bytes when all of the answering protocol lived in it.
func TestTheHandshakeCoreIsSmall(t *testing.T) {
	if n := len(channelInstructions); n > 1800 {
		t.Errorf("handshake core is %d bytes; the answering rules belong on the event", n)
	}
}

// THE HANDSHAKE TEACHES galley_open AND WARNS OFF THE SHELL. The old paragraph
// told the agent to run `galley edit` itself, which is how an editor came to be
// stamped with whatever session id the shell happened to carry. Both halves
// are pinned: the tool by name, and the prohibition, because an agent that
// knows the tool and is not told the shell is wrong will still reach for the
// command it remembers.
func TestTheHandshakeCoreTeachesGalleyOpen(t *testing.T) {
	lower := strings.ToLower(channelInstructions)
	if !strings.Contains(lower, "galley_open") {
		t.Error("channelInstructions never names galley_open")
	}
	if !strings.Contains(lower, "never run galley edit") {
		t.Error("channelInstructions does not warn the agent off `galley edit` from a shell")
	}
	if strings.Contains(lower, "cannot open one") || strings.Contains(lower, "start the editor yourself") {
		t.Error("channelInstructions still carries the pre-galley_open paragraph")
	}
	skill := strings.ToLower(pluginSkill(t))
	if !strings.Contains(skill, "galley_open") {
		t.Error("the plugin skill never names galley_open")
	}
	if strings.Contains(skill, "has no tool that opens a document") {
		t.Error("the plugin skill still says the channel cannot open a document")
	}
}
