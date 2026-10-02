package markdown_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/schuettc/galley/internal/docmodel"
	"github.com/schuettc/galley/internal/markdown"
)

// FuzzRoundTrip is the round-trip property test. It asserts the TRUE
// properties of Parse/Serialize, which are weaker than the obvious ones and
// were established the hard way across Tasks 3 and 4 — asserting the
// obvious ones instead just produces a corpus full of known classes and
// hides whatever real bug is underneath them.
//
// What is NOT asserted, and why:
//
//   - "Serialize is a fixed point on the first write" is false. Markdown
//     has no escape for CriticMarkup, so marker characters that survive one
//     write as literal text can be read as SYNTAX on the next one. The
//     document still SETTLES, it just takes another round — see
//     TestSerialize_LiteralMarkerText_Convergence in critic_test.go. Its two
//     cases now settle on the FIRST write (the per-span run stopped adjacent
//     spans fusing, which is what used to splice marker characters together),
//     but the class is not provably gone, so the harness still grants rounds.
//     Property (a) below is the true, weaker statement.
//
//   - "docmodel.Equal(d1, d2)" is false in two documented ways: mark ORDER
//     within an Inline is not canonicalized ({++a{--b--}c++} reads back
//     [Ins, Del] and writes a form that reads back [Del, Ins]), and
//     suggestion Attrs (author, at) have nowhere to live in the file at all
//     (the Attrs half is pinned by critic_test.go's
//     TestSerialize_DifferentAuthorsStayDistinctSpans; the mark-order half is
//     not pinned anywhere). The bytes still settle. What
//     must never happen is the AUTHOR'S WORDS going missing, which is what
//     property (b) checks instead — and checks more strictly than tree
//     equality would, because it survives every legitimate normalization.
//
//   - "text is preserved exactly" is false: collapsing a literal-marker
//     document can drop the punctuation between two markers. Property (b)
//     compares LETTERS AND DIGITS only, which is the line between "this
//     round trip lost some syntax it never had a spelling for" and "this
//     round trip ate a word".
//
// The three properties:
//
//	(a) the round trip CONVERGES to a byte-fixed point within three
//	    serializations;
//	(b) from the first serialization onward, every letter and digit is
//	    either still in the next serialization or was lifted into a
//	    comment on the way;
//	(c) every intermediate output reparses without error. No exemptions:
//	    "Serialize wrote a construct Parse refuses" is a file that will not
//	    load, and no shape of that is worth tolerating.
//
// Two further known classes, both pre-existing and both out of scope for
// this task, are tolerated by MOVING property (a)'s deadline rather than by
// dropping it: emphasis delimiters that CommonMark does not pair back the
// way the renderer emitted them, and CriticMarkup markers that survive one
// write as text and are read as syntax on the next. Both unwind one layer
// per round, so both buy rounds rather than exemptions. See slowRounds.

// Property (b) starts at the FIRST serialization, not at the source,
// because Parse-then-Serialize legitimately renumbers: a document starting
// "10. x" is an ordered list whose marker Serialize rewrites as "1.", and
// that is canonicalization, not loss. Everything after the first write is
// already canonical, so from there on nothing may go missing.
func FuzzRoundTrip(f *testing.F) {
	for _, seed := range fuzzSeeds(f) {
		f.Add(seed)
	}
	f.Cleanup(func() {
		// Only meaningful under plain `go test`, where the seed corpus runs
		// in this process. Under -fuzz the target runs in worker processes,
		// so the coordinator's counter stays at zero; the real signal there
		// is the absence of a crasher.
		if n := linkSplitSkips.Load(); n > 0 {
			f.Logf("skipped the alphanumeric property on %d input(s) holding a link split "+
				"across emphasis runs (known class — see "+
				"TestSerialize_LinkSpanningEmphasis_BecomesSeveralLinks)", n)
		}
		if n := criticSlow.Load(); n > 0 {
			f.Logf("granted %d input(s) extra rounds for CriticMarkup markers read back as "+
				"syntax (known class — see "+
				"TestSerialize_LiteralMarkerText_Convergence)", n)
		}
		if n := emphasisSlow.Load(); n > 0 {
			f.Logf("granted %d input(s) extra rounds for emphasis delimiters that do not "+
				"re-pair (known classes — see "+
				"TestSerialize_AdjacentEmphasisDelimiters_FuseAndLoseTheEmphasis, and "+
				"emphasis whose content edge is punctuation, which cannot close at all)", n)
		}
	})
	f.Fuzz(func(t *testing.T, src string) {
		doc, _, err := markdown.Parse([]byte(src))
		if err != nil {
			// Not a document this package accepts. Parse refusing input is
			// the designed behaviour, not a failure, and there is nothing
			// to round-trip.
			return
		}
		linkSplit := splitsALink(doc)
		// outs[k] is the (k+1)'th serialization: outs[0] = Serialize(doc),
		// outs[k] = Serialize(Parse(outs[k-1])).
		outs := [][]byte{markdown.Serialize(doc)}
		// lifted[k] is the text of the comments Parse pulled OUT of outs[k]
		// on its way to producing outs[k+1]. Serialize never writes a
		// comment back — they live in the sidecar by design — so text that
		// ends up inside "{>>...<<}" leaves the file, and property (b) has
		// to account for it or it reads as loss.
		var lifted []string
		// The known classes allowed extra rounds, both pre-existing and
		// both out of scope for this task, are the EMPHASIS ones: the
		// renderer emits "*" and "**" from the mark set without checking
		// that CommonMark will pair them back the same way, and where it
		// will not, the next read spells some of them as literal text
		// instead. It unwinds ONE delimiter run per round.
		//
		// Two shapes are known. Adjacent segments whose delimiters land
		// flush fuse into one longer run
		// (TestSerialize_AdjacentEmphasisDelimiters_FuseAndLoseTheEmphasis),
		// and a run whose content edge is punctuation cannot close at all
		// (Bold("x}") followed by text("a") has no CommonMark spelling —
		// "**x}**a" cannot close, so it degrades to escaped literal text).
		// Both unwind at the same rate, and
		// TestSerialize_FusedEmphasis_CostsARoundEach measures it: one
		// round per run.
		//
		// The other slow class is CriticMarkup's, and it is slow for the
		// same reason: markdown has no escape for a marker, so marker
		// characters that survive one write as literal text are read as
		// SYNTAX on the next, one layer per round. Task 4 pinned it in
		// TestSerialize_LiteralMarkerText_Convergence and named convergence
		// — not first-write stability — as the true property this fuzz
		// should be built on.
		//
		// So the budget is the base plus one round per marker or delimiter
		// the output actually contains — recomputed EVERY round, because a
		// document can grow one it did not have on the first write. Every
		// assertion still applies; only the deadline moves, and only for
		// documents holding the syntax in question. An input with no "*",
		// "_" or CriticMarkup marker gets the base budget and nothing more,
		// which is what kept the heading-erosion and list-merge bugs
		// visible.
		rounds := roundBudget(outs[0])
		for len(outs) < rounds {
			prev := outs[len(outs)-1]
			d, comments, err := markdown.Parse(prev)
			if err != nil {
				// No exemption. Task 4 left one shape here — removing
				// markers joined "<" and "b>" into "<b>", which Parse
				// refuses — and Task 5 fixed it in the escape layer
				// (opensAngleMarkup), so this task's first draft kept a
				// COUNTED SKIP as a backstop for the rest of that family.
				//
				// That was the wrong shape for a backstop. Under -fuzz the
				// target runs in worker processes, so the counter is
				// invisible exactly when it matters — and a live member of
				// the family was hiding behind it: a link destination
				// holding a newline has no spelling in either form, so the
				// "<" opened raw HTML and the file stopped loading. Silence
				// read as health. Failing is the only report a fuzz target
				// can actually deliver.
				t.Fatalf("serialization %d does not reparse: %v\n src:  %q\n out:  %q",
					len(outs), err, src, prev)
			}
			linkSplit = linkSplit || splitsALink(d)
			lifted = append(lifted, commentText(comments))
			out := markdown.Serialize(d)
			outs = append(outs, out)
			if bytes.Equal(out, prev) {
				break
			}
			if grown := roundBudget(out); grown > rounds {
				rounds = grown
			}
		}
		// (a) converged: the last two serializations agree, which makes the
		// last one a fixed point by definition.
		if n := len(outs); n < 2 || !bytes.Equal(outs[n-1], outs[n-2]) {
			t.Errorf("round trip did not converge within %d serializations:\n src: %q\n%s",
				rounds, src, formatRounds(outs))
		}
		// (b) no letters or digits lost, from the first serialization on.
		if linkSplit {
			// A link whose content changes emphasis is written as several
			// adjacent links, one per emphasis run, REPEATING THE
			// DESTINATION each time — so the count of letters and digits in
			// the bytes tracks how many runs there happen to be, not what
			// the document says. The document itself is unchanged
			// (docmodel.Equal agrees) and the bytes still have to converge;
			// it is only this comparison that the repetition makes
			// meaningless. Pinned by
			// TestSerialize_LinkSpanningEmphasis_BecomesSeveralLinks.
			linkSplitSkips.Add(1)
			return
		}
		for k := 0; k+1 < len(outs); k++ {
			// Everything alphanumeric in one serialization is either still
			// in the next one or was lifted into a comment on the way. That
			// second half is not a loophole: it is where the marker-soup
			// class actually goes. "{=={>>0<<}==}" is literal text on one
			// write and a real COMMENT on the next read, and the "0" leaves
			// the file — but it leaves it for the sidecar, which is the
			// designed behaviour, not a document eating a digit.
			want := sortedAlnum(outs[k])
			got := sortedAlnum(append(append([]byte{}, outs[k+1]...), lifted[k]...))
			if got != want {
				t.Errorf("serialization %d lost or gained alphanumeric content:\n src:  %q\n got:  %q\n want: %q\n lifted into comments: %q\n%s",
					k+2, src, got, want, lifted[k], formatRounds(outs))
				break
			}
		}
	})
}

// maxSerializations is property (a)'s bound. Three is not arbitrary: the
// worst known-good case takes two writes to settle (the first write emits
// literal marker characters, the second read interprets them, the third
// write is identical to the second), so three serializations is one more
// than anything documented needs. A document that still moves on the third
// is either a new class or a genuine non-convergence, and either one is
// worth a crasher.
const maxSerializations = 3

// maxRoundBudget is the hard ceiling on property (a)'s deadline, however
// much slow-class syntax an input contains. It exists for two reasons, and
// neither is that the tolerance is wrong.
//
// COST: the budget scales with the input, and the input can be large. A 14KB
// document of fused emphasis earns about a thousand rounds and takes ~27
// seconds to settle — and if such an input ever lands in the committed seed
// corpus, every `go test`, every pre-push hook and every CI run pays it.
//
// The cap is not a far-off backstop, and the 14KB figure above should not be
// read as one: it binds at TWENTY-FOUR slow-syntax runs, whatever the byte
// count. Measured — 24 repetitions of "*a* " (96 bytes), of "{++a++} " (192
// bytes), or of a sentence with one emphasis span in it (~1KB) all reach it.
// A capped input is not a failing input; it just gets the ceiling rather than
// its full earned tolerance, which is why the ceiling has to stay far above
// what anything has actually needed (five).
//
// TERMINATION: a future regression that oscillates while GAINING a marker
// each round raises the budget as fast as it spends it, and the target
// stops failing and starts hanging. A hang is the worst outcome a test can
// have, because it reads as slowness rather than as a bug.
//
// Fifty keeps the whole tolerance — the most any input has actually needed
// is five — while turning both pathologies back into a crasher, which is
// what a fuzz target is for.
const maxRoundBudget = 50

// linkSplitSkips counts inputs whose alphanumeric property was skipped for
// the split-link class. Only observable under plain `go test`: under -fuzz the
// target runs in worker processes, so the coordinator never sees them.
var linkSplitSkips atomic.Int64

// splitsALink reports whether d holds a link whose content changes emphasis
// part-way through — two adjacent inlines under the same destination with
// different bold/italic. Serialize writes that as one "[...](dest)" per
// emphasis run, so the destination appears once per run in the bytes.
//
// A code span does NOT split a link (Code is not part of the wrapper that
// segments a run), so it is deliberately not checked here.
func splitsALink(d docmodel.Doc) bool {
	split := false
	docmodel.Walk(d, func(_ []int, b *docmodel.Block) {
		for i := 1; i < len(b.Inlines); i++ {
			prev, cur := b.Inlines[i-1], b.Inlines[i]
			if !prev.Has(docmodel.Link) || !cur.Has(docmodel.Link) {
				continue
			}
			if prev.Attr(docmodel.Link, "href") != cur.Attr(docmodel.Link, "href") {
				continue
			}
			if prev.Has(docmodel.Bold) != cur.Has(docmodel.Bold) ||
				prev.Has(docmodel.Italic) != cur.Has(docmodel.Italic) {
				split = true
			}
		}
	})
	return split
}

// emphasisSlow and criticSlow count inputs granted extra rounds for the two
// slow classes. Same caveat as linkSplitSkips above: only observable under
// plain `go test`, since -fuzz runs the target in worker processes.
var (
	emphasisSlow atomic.Int64
	criticSlow   atomic.Int64
)

// roundBudget is how many serializations out is allowed before property (a)
// calls it a failure: the base, plus what the slow classes earn, capped.
func roundBudget(out []byte) int {
	n := maxSerializations + slowRounds(out)
	if n > maxRoundBudget {
		return maxRoundBudget
	}
	return n
}

// slowRounds is the extra convergence budget out earns: one round per
// emphasis delimiter run and one per CriticMarkup marker, the two syntaxes
// whose known classes unwind a layer at a time.
func slowRounds(out []byte) int {
	n := emphasisDelimiterRuns(out)
	if n > 0 {
		emphasisSlow.Add(1)
	}
	if m := criticMarkers(out); m > 0 {
		n += m
		criticSlow.Add(1)
	}
	return n
}

// criticMarkers counts CriticMarkup marker sequences in out. It cannot tell
// a marker the renderer wrote for a real suggestion (which is stable) from
// one that is literal text about to be re-read as syntax (which is not), so
// it counts both — an input with suggestions in it simply gets a budget it
// does not need.
//
// A NOTE's own "{>>"/"<<}" is excluded, because that pair is not the slow
// class this budget exists for. Serialize emits a Note as "{>>…<<}" and Parse
// reads it straight back as the same Note — it is stable on the FIRST write,
// with nothing to unwind. Counting it granted every note 2 rounds it cannot
// use, and a document with 24 notes reached maxRoundBudget and silently
// loosened property (a) to the cap: the convergence assertion would have
// stopped failing on real non-convergence.
func criticMarkers(out []byte) int {
	// Counted with the backslashes removed. Escaping splits a marker
	// ("<<}" can go out as "<\\<}"), and a marker the count misses is a
	// round of budget the document does not get — which showed up as a
	// deeply nested marker soup running out of rounds one layer short.
	bare := bytes.ReplaceAll(out, []byte{'\\'}, nil)
	n := 0
	for _, marker := range []string{"{++", "++}", "{--", "--}", "{==", "==}", "{~~", "~~}", "{>>", "<<}"} {
		n += bytes.Count(bare, []byte(marker))
	}
	return n - 2*standaloneNoteLines(bare)
}

// standaloneNoteLines counts lines that are nothing but one note — exactly
// what Serialize writes a docmodel.Note as, and exactly what Parse reads back
// as the same Note. Each contributes one "{>>" and one "<<}" that criticMarkers
// must not charge the convergence budget for.
func standaloneNoteLines(bare []byte) int {
	n := 0
	for _, line := range bytes.Split(bare, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) < 6 || !bytes.HasPrefix(line, []byte("{>>")) || !bytes.HasSuffix(line, []byte("<<}")) {
			continue
		}
		// One note, not a nest: the first closer must be the last one.
		if bytes.Index(line[3:], []byte("<<}")) != len(line)-6 {
			continue
		}
		if bytes.Contains(line[3:len(line)-3], []byte("{>>")) {
			continue
		}
		n++
	}
	return n
}

// emphasisDelimiterRuns counts maximal runs of '*' or '_' in out that the
// renderer emitted as DELIMITERS — an escaped star is literal text, so
// skipping the character after a backslash is what tells the two apart.
//
// A code span's content is neither escaped nor a delimiter, so "`*`" is
// counted when it should not be. That spends a round of budget on an input
// that did not need one, and can never turn a real failure into a pass,
// which is the right direction for a tolerance to err in.
func emphasisDelimiterRuns(out []byte) int {
	runs, prev := 0, byte(0)
	for i := 0; i < len(out); i++ {
		c := out[i]
		if c == '\\' {
			i++
			prev = 0
			continue
		}
		if c != '*' && c != '_' {
			prev = 0
			continue
		}
		if c != prev {
			runs++
			prev = c
		}
	}
	return runs
}

// alnum keeps only ASCII letters and digits. That is deliberately narrow:
// punctuation is exactly what the known lossy classes drop (a marker's
// closer, an emphasis delimiter that fused with its neighbour), and
// treating its loss as a failure would bury the corpus in cases Tasks 3 and
// 4 already characterized. Letters and digits are the author's words, and
// losing one of those is a bug in any class.
func alnum(b []byte) string {
	var out strings.Builder
	for _, c := range b {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			out.WriteByte(c)
		}
	}
	return out.String()
}

// sortedAlnum is alnum as a MULTISET rather than a sequence. Order is not
// comparable across the two sides: a comment's text is lifted out of the
// middle of a block, so what is left behind and what went to the sidecar
// interleave differently than they appeared. What must hold is that every
// letter and digit is still somewhere.
func sortedAlnum(b []byte) string {
	s := []byte(alnum(b))
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return string(s)
}

// commentText concatenates the text of the comments Parse lifted out of a
// document.
func commentText(comments []markdown.InlineComment) string {
	var b strings.Builder
	for _, c := range comments {
		b.WriteString(c.Text)
	}
	return b.String()
}

func formatRounds(outs [][]byte) string {
	var b strings.Builder
	for i, out := range outs {
		b.WriteString(" out")
		b.WriteByte(byte('1' + i))
		b.WriteString(": ")
		b.WriteString(strconv.Quote(string(out)))
		b.WriteByte('\n')
	}
	return b.String()
}

// fuzzSeeds is the seed corpus: every golden fixture, plus the inputs that
// Tasks 3 and 4 found by hand or by stress probe. Seeding with the known
// classes is what keeps the fuzzer from spending its budget rediscovering
// them — it starts already inside the interesting region and mutates
// outward from there.
func fuzzSeeds(f *testing.F) []string {
	f.Helper()
	files, err := filepath.Glob("testdata/*.md")
	if err != nil {
		f.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		f.Fatal("no fixtures found")
	}
	seeds := make([]string, 0, len(files)+len(handSeeds))
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("read %s: %v", path, err)
		}
		seeds = append(seeds, string(b))
	}
	return append(seeds, handSeeds...)
}

// handSeeds are the counterexamples the earlier tasks paid for, kept here
// so a mutation that is one byte away from a known cliff starts on the
// cliff rather than having to find it again.
var handSeeds = []string{
	// FRONT MATTER, AND THE "---" SHAPES EITHER SIDE OF IT. The block itself
	// is carried verbatim, so the interesting seeds are the ones where the
	// SERIALIZER'S OWN OUTPUT is what the next Parse has to read: a thematic
	// break is written "---", so a document opening with one — with any later
	// bare "---" line, in a rule or inside a fence — is written as an opener
	// and a closer with the document between them. Property (a) is exactly the
	// check for that: it converges only if Parse reads back what Serialize
	// meant.
	"---\ntitle: x\n---\n",
	"---\ntitle: x\n---\n\nprose\n",
	"---\ntitle: x\n---\nprose\n",
	"+++\ntitle = \"x\"\n+++\n\nprose\n",
	"---\nlist:\n  - a\n  - b\n---\n\nprose\n",
	"---\n---\n",
	"---\n\nprose\n",
	"***\n\n***\n",
	"***\n\nprose\n\n***\n",
	"***\n\n```sh\n---\n```\n",
	"+++\nprose\n",
	"---\r\ntitle: x\r\n---\r\n\r\nprose\r\n",
	// The note grammar, which shipped with no fuzz coverage at all: not one
	// seed was a plain {>>note<<}, and @document/@block — splitNoteMarker,
	// noteBody, startsWithMarker, the branchiest code in the package — was
	// never fuzzed, only driven by a hand-written table.
	//
	// The first four are the shapes that DELETED a note from the file before
	// noteline.go: a note flush under a paragraph, and a paragraph holding
	// more than one. A seeded fuzzer would plausibly have found both.
	"{>>note<<}\n",
	"para\n{>>note<<}\n",
	"{>>one<<}\n{>>two<<}\n",
	"{>>one<<} {>>two<<}\n",
	"a\\\n{>>n<<}\n",
	"{>>note<<}  \nx\n",
	"{>>@document overall<<}\n",
	"{>>@block literal<<}\n",
	"{>>@documentation is missing<<}\n",
	"{>>@document<<}\n",
	"{>>@block @document<<}\n",
	"![a](d.png)\n\n{>>on the figure<<}\n",
	"- {>>in a list<<}\n",
	"> {>>in a quote<<}\n",
	"```\n{>>in a fence<<}\n```\n",
	"`{>>in a code span<<}`\n",
	"**{>>bold<<}**\n",
	// A note in a TABLE CELL, which no seed and no test covered: renderCell
	// had no Note case, so "| x | {>>n<<} | z |" wrote back "| x | n | z |"
	// and the comment left the .md for the sidecar alone. Every column
	// position, plus the marker forms, since the escape has to be written
	// through escapeCellText as well as through renderPlan.
	"| a | b | c |\n| --- | --- | --- |\n| x | {>>n<<} | z |\n",
	"| a | b | c |\n| --- | --- | --- |\n| {>>n<<} | y | z |\n",
	"| a | b | c |\n| --- | --- | --- |\n| x | y | {>>n<<} |\n",
	"| {>>n<<} | b |\n| --- | --- |\n| x | y |\n",
	"| a | b |\n| --- | --- |\n| x | {>>@document n<<} |\n",
	"| a | b |\n| --- | --- |\n| x | {>>@block @document n<<} |\n",
	"| a | b |\n| --- | --- |\n| x | {>>a\\|b<<} |\n",
	"| a | b |\n| --- | --- |\n| x | {>>one<<} {>>two<<} |\n",
	"| a | b |\n| --- | --- |\n| x | t {>>beside<<} |\n",
	// The two literal-marker cases from
	// TestSerialize_LiteralMarkerText_Convergence. They used to be the only
	// documented inputs needing a third serialization to settle; both are
	// first-write stable now, and they stay in the corpus as the shapes that
	// would regress first if adjacent spans ever fused again.
	"x{--{~~}{--b~>~~}x\n",
	"{=={>>==}{==<<}==}\n",
	"{=={>>==}{==0<<}==}\n",
	"{{>{>><<}><<}>><<}\n",
	"{{>{>{>><<}><<}><<}>><<}>\n",
	// COMMENT ID MARKS. testdata/comment-ids.md seeds the shapes the serializer
	// writes; these are the near-misses either side of the binding rule: a
	// space before the mark (an ordinary note), a malformed token, a mark in a
	// heading, a highlight holding a code span that spells "==}" (no spelling,
	// so its mark is not written back), and one ID over adjacent pieces.
	"a {==b==}{>>@comment cm-0123456789abcdef<<} c\n",
	"a {==b==} {>>@comment cm-1<<} c\n",
	"a {==b==}{>>@comment two words<<} c\n",
	"a {==b==}{>>@comment<<} c\n",
	"# {==t==}{>>@comment cm-1<<}\n",
	"{==`x==}`==}{>>@comment cm-1<<}\n",
	"{==a==}{>>@comment cm-1<<}{==b==}{>>@comment cm-1<<}\n",
	"**{==y==}{>>@comment cm-1<<}**\n",
	"{>>@comment cb-1<<}\n",
	"para\n{>>@comment cb-1<<}\n",
	"{>>@block @comment cb-1<<}\n",
	"- {>>@comment cb-1<<}\n",
	// The splice shapes: text either side of a removed marker becoming a
	// construct Parse refuses, which is the one failure mode that stops a
	// file from loading.
	"x}<{----}b>x\n",
	"\\<0@0>\n",
	"\\<a href=\"x\">y\n",
	// CriticMarkup's four spans, its comment, and the shapes fix round 1
	// found: a closer inside the content, and a substitution with no safe
	// spelling.
	"{++a++}{--b--}{==c==}{~~d~>e~~}{>>f<<}\n",
	"{--~~}--}\n",
	"{--a--}{++b++}\n",
	"`a`{--`b`--}\n",
	"{=={++b++}==}\n",
	// The comment seams: a {>>note<<} leaves whitespace on both sides of
	// itself when it is lifted out, and collapseNoteSeams removes one of
	// them. These are the shapes where getting that wrong either fuses two
	// words or reaches into a code span or a hard break. See
	// TestParse_Comment_SeamWhitespaceCollapses.
	"A {>>n<<} B\n",
	"A {>>one<<} {>>two<<} B\n",
	"a  {>>n<<}  b\n",
	"a {>>n<<}b\n",
	"`x `{>>n<<} b\n",
	"a {>>n<<}\\\nb\n",
	"a {>>n<<} \\\n b {>>m<<} c\n",
	// Emphasis and code adjacency — the delimiter-run fusion hazards.
	"*a **b***c*\n",
	"[**0*0* 0**0](0)\n",
	"*\x00 **0***\x00*\n",
	"*0\xab*0**0** 000*0*\n",
	"**a**{--**b**--}\n",
	"`a``b`\n",
	"*a***b**\n",
	// Line-leading block markers, this task's own fix.
	"\\- x\n",
	"1\\. x\n",
	"\\# x\n",
	"\\> x\n",
	"a\\\n\\- b\n",
	"- \\- x\n",
	"\\---\n",
	// Escaping proper: backslashes, brackets that nearly make a link, and
	// the UTF-16/rune hazard the CLAUDE.md notes call out.
	"a\\*b [x] (y)\n",
	"back\\slash \\\\ snake_case_name\n",
	"☕😀{++😀++}\n",
	// Block structure, so the fuzzer has list/quote/heading/fence material
	// to splice marker soup into.
	"- a\n- b\n\n1. c\n\n> q\n\n# h\n\n```go\nx\n```\n",
	// Tables (phase 1e). The delimiter row is the only line in markdown
	// whose meaning depends on the line ABOVE it, so most of these are
	// about what a table does to its neighbours rather than about a table.
	"| a | b |\n| --- | --- |\n| 1 | 2 |\n",
	"|a|b|\n|-|-|\n|1|2|\n",
	"| a | b |\n| :-- | --: |\n| 1 | 2 |\n",
	"| a |\n| :-: |\n| |\n",
	"| a\\|b | c |\n| --- | --- |\n| `x\\|y` | z |\n",
	"| **a** | `b` | [c](d) |\n| --- | --- | --- |\n| {--e--} | {++f++} | {==g==} |\n",
	"| a | b |\n| --- | --- |\n| 1 |\n| 1 | 2 | 3 |\n",
	"text\n| a | b |\n| --- | --- |\n",
	"| a | b |\n| --- | --- |\ntext\n",
	"| a | b |\n| --- | --- |\n\n| c | d |\n| --- | --- |\n",
	"| a |\n| --- |\n# h\n",
	"> | a |\n> | --- |\n> | b |\n",
	"- | a |\n  | --- |\n  | b |\n",
	"|  |  |\n| --- | --- |\n",
	"☕😀|a\n-|-\n",
}

// TestRoundBudget_IsCapped is fix round 1's Important 3. The convergence
// budget scales with the input, which is what makes the slow-class
// tolerance honest — but unbounded it is two failures waiting.
//
// A large enough document of fused emphasis earns roughly a round per
// delimiter run, so a 14KB one asks for about a thousand serializations and
// takes tens of seconds. Put that in the committed seed corpus and every
// `go test`, every pre-push hook and every CI run pays it. Worse, a future
// regression that oscillates while GAINING a marker each round raises the
// deadline exactly as fast as it spends it: the target stops failing and
// starts hanging, which reads as slowness rather than as a bug.
//
// The cap turns both back into a crasher, which is the only thing a fuzz
// target can usefully do with them.
func TestRoundBudget_IsCapped(t *testing.T) {
	if got := roundBudget(bytes.Repeat([]byte("*a* "), 4000)); got != maxRoundBudget {
		t.Errorf("roundBudget(14KB of emphasis) = %d, want the cap %d", got, maxRoundBudget)
	}
	if got := roundBudget([]byte("plain text with no slow syntax\n")); got != maxSerializations {
		t.Errorf("roundBudget(no slow syntax) = %d, want the base %d", got, maxSerializations)
	}
	if got := roundBudget([]byte("*a ****b*** c*\n")); got <= maxSerializations || got >= maxRoundBudget {
		t.Errorf("roundBudget(a little fused emphasis) = %d, want between %d and %d exclusive — "+
			"the tolerance must still buy rounds, and must not reach the cap for a small input",
			got, maxSerializations, maxRoundBudget)
	}
	// The cap has to stay well clear of what real inputs need, or it stops
	// being a backstop and starts being the property. The most any input
	// has been measured to need is five.
	if maxRoundBudget < 20 {
		t.Errorf("maxRoundBudget = %d, too tight to leave the measured tolerance intact", maxRoundBudget)
	}
}

// A table is not a slow class. The two syntaxes that unwind a layer per round
// — emphasis delimiters and CriticMarkup markers — are unwinding a
// SPELLING ambiguity; a table's normalizations (column count, alignment,
// spacing) all resolve in a single write, so the base budget covers it. This
// pins that, so a future table shape that needs a fourth serialization fails
// loudly instead of quietly borrowing budget some other syntax earned.
func TestRoundBudget_ATableEarnsNoExtraRounds(t *testing.T) {
	src := []byte("| a | b |\n| :-- | --: |\n| 1 | 2 |\n")
	if got := roundBudget(src); got != maxSerializations {
		t.Errorf("roundBudget(a table) = %d, want the base %d", got, maxSerializations)
	}
}
