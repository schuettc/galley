# Reviewing a document

This is the reviewer's side of galley: what the browser page is, what each control does, and what happens to your words after you press **Revise**.

It assumes galley is installed and that an agent session is attached to the document. The agent half — the plugin, `galley channel`, `galley wait` — is in [plugin/README.md](../plugin/README.md).

## The page

```sh
galley edit docs/plan.md
```

That opens your document in a browser page. The document is the page: there is no preview pane and no side-by-side source. You are editing the file.

The model is one paragraph. You edit the draft directly, and those edits are the document. You attach instructions to spans of text, or to the whole document, for work you want the agent to do. Pressing **Revise** commits the draft as a version and sends the instructions as one round. The agent revises the file in place; its saves stream back into your page as it works, and its return commits the next version. You read what changed, give another instruction if you need one, and repeat until you approve.

Every round is a version. Nothing is a proposal you have to accept.

## The bar

The bar across the top carries, left to right: the document's name, `Instructions · N`, `History`, the status readout, then the `live` switch, `⏸ hold`, and the primary button.

The status readout is one line, composed newest-first, and it ellipsizes from the right:

```
requesting a revision… · round 3 · draft · saved just now
```

- The first clause, when there is one, is the server's reply to whatever you last pressed.
- Then where the document is: `round N` — the number of rounds this document has been through — or `v1` before the first round exists, followed by `draft` while the document is yours, `with the agent` while it is not, or `settled` once the review is over.
- Then the connection: `saved just now` (or `2m ago`, `1h ago`), or plain `connected` before anything has been written, or `connecting…` if the socket is not up.

`Instructions · N` counts the instructions you have written and not yet sent. Pressing it opens the list. `History` opens the record of rounds; it turns amber when something has arrived that you have not read.

## Direct edits and instructions

There are two things you can do to the document, and they are not the same thing.

**A direct edit applies immediately.** Type, delete, paste, reflow a paragraph — it is in the document as you do it, it is projected to the `.md` file, and nobody approves it. It is not a proposal and there is no accept/reject cycle. While the round is still open, a deletion leaves a struck-through ghost of the removed text where it was and an insertion glows, so you can see what your own hand has done this round; those marks are drawn on the page and are never written to the file. They go away when the round is sent.

**An instruction is work for the agent.** It is what you write when the change is not yours to make — a section that needs rewriting, a claim you want checked, a table you want expanded. Instructions accumulate until you press Revise, and then they travel as one round.

The rule of thumb: fix it yourself if you can type the fix; leave an instruction if describing the change is the work.

## Leaving an instruction on a span

Select the words you mean. A small composer appears just below the selection with one button, **Add instruction**. Press it and the box opens, headed with what you selected:

```
INSTRUCTION · ON "the retry budget is generous"
```

Type into the box — placeholder `what about it?` — and press Enter to file it. Shift-Enter breaks a line instead. `cancel` closes the composer, and so does Esc; the composer says `esc cancels` beside the button so you do not have to guess.

Some places refuse an anchor. A selection touching a code fence, a table, front matter, or a display-math block replaces the **Add instruction** button with the reason, in the same voice the editor uses when it refuses a keystroke there:

> a code fence is literal text — galley never rewrites one, so it is read-only here
> select and copy still work — edit fenced code in your own editor

Those blocks render and are selectable and copyable, and none of them is editable by typing. A table, front matter, and a display-math block take no instruction at all. A **code block is the exception**: you cannot hang a *selection* instruction inside it, but you can instruct it as a whole — see below.

## Instructing a whole code block

Hover a code block and a small `{}` grip appears in the left gutter, distinct from the other gutter affordances. Click it and the instruction box opens on the whole block — there is no selection, because a fence is one unit. Type the instruction and file it as you would any other. It anchors to the block, appears as a card in the rail beside it, and travels with the round; the agent rewrites the block's contents in reply. You still cannot type inside the fence or hang an instruction on a selection within it — the grip instructs the block as a whole, which is the one thing a fence can carry.

## Leaving an instruction on the whole document

The first card at the top of the instruction rail is **+ instruction on the whole document**. Press it, type into the box (`add an instruction on the whole doc…`), and press Enter. Shift-Enter breaks a line instead. Use it for anything without a place in the text: the tone, an argument that is missing, a structure you want changed.

Line breaks and blank lines are kept in every instruction, whichever box you wrote it in. The card shows them, the agent receives them, and so do `galley round`, `galley pending`, the ledger and History.

## Changing your mind before you send

Every instruction becomes a card in the rail, headed `instruction · <what it is about> · <age>` — the anchor is quoted for a span, or reads `whole document`.

Each card carries two verbs:

- **edit** opens the words back up in a box with `save` and `cancel`. Enter saves and Shift-Enter breaks a line, as in every other instruction box. Saving is one mutation, not a delete and a re-file. An empty save is refused: `an instruction with no words is a delete`.
- **delete** takes two clicks. The first arms it — the label becomes `delete?` and the card says `this removes the comment and its mark — click again`. The second click deletes. The arming lapses after four seconds on its own, so a card left armed is not a trap for the next click.

If you delete all the words an instruction on a span was about, the instruction goes with them: deleting the sentence takes back the instruction about it, and it is not sent. Undo the deletion before you send and it comes back.

An instruction on a block or a figure has a mark of its own instead of highlighted words. If that mark is removed, the card does not disappear. It moves into an `unplaced · its words were removed` group, so you can see which instruction is now floating, and it is still sent with the round.

## Sending

The primary button on the right is the verdict.

With instructions pending it reads **`Revise · N ▾`** — the count is the number of instructions the server is holding, and the `▾` says the press opens a menu rather than sending. The menu has two exits:

- **Revise** — hand the document to the agent with everything still pending. This is the ordinary press.
- **Revise & Approve** — send these instructions and approve *only after* the agent successfully applies them.

**Revise & Approve** is a real conditional, not an optimistic approve. The review closes only when the agent returns with `answered`. If it returns `declined` or `failed`, or reports `cannot`, the review stays open and the document comes back to you.

While a revision is out, the button counts: `revising · 12s`. It counts until the revision *lands*, not until the request returns, so a six-second revision shows six seconds. The document is read-only while the agent holds it and the readout says `with the agent`. A **`✕ cancel`** button appears beside the primary; it takes the document back, keeping the agent's partial work as that round if the file parses.

With no instructions pending the button reads **`Approve`** and the press sends the approve directly — there is nothing to choose between. Once it lands, the label becomes `approved`, the bar shows the terminal line (`Approved 14:32 · review closed`), and the page stops taking input.

## Reading what came back

When a round lands, the readout says so and the `History` chip goes amber:

```
v4 · agent revised · see History
```

A strip announces it with the counts, taken from the server and not from anything the page drew:

```
round 3 answered · v4 · 3 changes
```

If the agent could not do what you asked, the round is still cut — with the document unchanged — and it says so instead:

```
round 3 · the agent could not · v4
```

The reason travels with it (`v4 · could not: the file it refers to does not exist in this repo`). A `cannot` is a report, not a conversation: you answer it with a different instruction in a new round.

Pressing `History` after an arrival lands you on that round's changes. Pressing it cold gives you the landing — every round, newest first, one card per exchange, each carrying `ROUND n · 4M AGO`, the instruction that produced it (`→ …`) and the agent's own sentence about the revision (`← …`), over `v4 · 3 changes`. `V1 · STARTING VERSION` sits at the foot: the file as galley opened it, which is not a round anybody had. With nothing there yet it says `No history yet — the first Revise creates a version.`

Inside a round the sub-bar reads `ROUND 3 · V3 → V4` and offers two views:

- **changes** — the round's document with the moved passages marked in place. This is the default; opening a round is already the question it answers.
- **side by side** — `v3 · before` and `v4 · after`. It is unavailable on v1, which has nothing to sit beside.

The rail beside the paper carries one card per change, `CHANGE 2 OF 3 · <nearest heading>`, with the instruction it answers and the agent's note on it. Clicking or pressing Enter on a card scrolls to that change on the paper and pins it; Esc releases the pin, and Esc again leaves History. A round that moved nothing says `identical — no changes in this round`.

History is a reading mode and nothing in it changes the draft — the readout says `reading round 3 · draft is untouched` while you are there. The primary button becomes `← back to draft`, and the scroll position you left is restored.

### Restoring an older version

At the right of the sub-bar, quietly, is **`restore v3 as draft`**. It copies that version over your current draft. It arms like the delete verb does: the first click changes it to `replace draft?`, the second sends, and the arming lapses after four seconds. It does not amend or delete any round — the next send records a normal new round from the restored draft — and the status line confirms `restored from v3 · not sent`.

It refuses, with the reason, when:

- there are unsent instructions — *send or delete the current instructions before restoring a version*;
- the agent holds the document — *the agent is revising — the document is read-only until it returns (or cancel the handoff)*;
- a revision you asked for has not landed yet — *a revision is still landing — wait for it before restoring a version*;
- the review is sealed;
- there is no such version.

## Live mode

The `live` switch decides whether the agent hears anything before you press Revise.

Off (the default), the agent hears nothing until you press Revise. On, the agent is woken whenever the document settles — you keep editing and it keeps working, without a press. The switch's tooltip says which state you are in and what a click does about it.

In live mode a **`⏸ hold`** button appears beside it. Holding keeps new arrivals out of the rail; they still land in the document, they just wait for a card. The button then reads `▶ release · 2` with the number waiting, and releasing announces them in one batch. Hold is a queue over what the rail shows, never a gate on the document.

## Keyboard

| Key | What it does |
|---|---|
| `Esc` | Closes whatever is open, topmost first: a refusal note, the mark bubble, the composer, the verdict menu, the instruction sheet, History's pinned change, then History itself, then the whole-document panel. With nothing open, it takes focus out of the text and hands it back to the page. |
| `Enter` | In any galley text box, files what you typed. In a card's edit box, saves it. |
| `Shift-Enter` | Breaks a line instead of filing, in every instruction box: the span composer, the whole-document box and a card's edit box. |
| `Enter` / `Space` | On a focused card, reveals what that card is about. |
| `Cmd-Z` / `Cmd-Shift-Z` | Undo and redo your own edits. |
| `j` / `k` | **Currently step nothing** — see the note below. Inert while History is open. |

Two notes on that table. The document itself is an ordinary editable surface, so `j` and `k` are letters while the caret is in the text — press Esc first to hand focus back to the page. And `j`/`k` (and the narrow bar's `↓ next`) currently walk a list that is empty in the rounds workflow: they step nothing. Reach instructions by clicking their cards, by Tab, or through `Instructions · N`.

## What galley will not accept in the document

galley's markdown is CommonMark plus tables, front matter, and display math. Seven constructs are refused rather than silently mangled, because each one means something galley's document model cannot carry:

| Construct | What the refusal says |
|---|---|
| Raw HTML, block or inline | `raw HTML not supported (line 1)` |
| Footnotes | `footnote not supported (line 1)` |
| Reference-style link definitions | `LinkReferenceDefinition not supported (line 3)` |
| Autolinks (`<https://example.com>`) | `AutoLink not supported (line 1)` |
| Definition lists | `definition list not supported (line 2)` |
| `:::` directive blocks | `::: directive block not supported (line 1)` |
| `> [!NOTE]` callouts | `> [!NOTE] callout not supported (line 1)` |

A bare URL in prose is fine; it is the angle-bracket form that is refused.

You will meet this in two places. `galley edit` refuses to open a file that contains one of them, naming the construct and the line. And while the agent holds the document, a save it cannot parse is *held* rather than imported — the file is on disk, but it is not on your page, and the readout says so:

```
round 3 · with the agent · agent is revising — its last save is held (not yet importable)
```

The agent's next save fixes it, and the held save imports then. The message stands until it does, so a silence there never reads as a lost save.

## Where your words live

**The document** is your `.md` file. galley owns it and writes it back continuously — it is what your editor, your build and git all see.

**Versions** are in `.galley/versions/<document>/`: `0001.md`, `0002.md`, one clean markdown file per round, plus `rounds.jsonl` with one line per round recording who moved it, when, why it was cut, and the instruction that produced it. They are full copies, not diffs, and the directory is gitignored — the history is insurance, not something you commit.

**Unsent instructions** are in `.galley/versions/<document>/pending.json`. Every add, edit and delete writes that file before it writes the `.md`, so stopping galley, or a crash, loses none of them. The next `galley edit` puts each one back in place, and one whose place is gone shows as unplaced. Pressing Revise sends them as the round and empties the file.

While an instruction is unsent, the `.md` carries only a short ID mark at its place: `{>>@comment cm-…<<}` straight after the highlighted words, a `{>>@comment cb-…<<}` line after a block, and nothing at all for an instruction on the whole document. The words are never in the `.md`. galley owns these marks. A `{>>note<<}` you type into the file yourself is not an instruction, and galley drops it: one inside a sentence at the next save, one on a line of its own when you press Revise.

Every stored version is a clean document. The ID marks are addresses, not prose: they are removed from the projection when you press Revise, so no round marker and no diff is ever stored in a version. Diffs are computed between versions when you ask to read one.

**The ledger** is `<repo>/.galley/decisions.jsonl` — committed, append-only, one JSON line per decision, merged with `merge=union` so a rebase keeps both sides. Your instructions are recorded there with the round that carried them. `galley ledger` reads it. A document edited outside a git repository has no ledger.

## Reviewing an HTML page

`galley edit` accepts `.html` and `.htm` files (case-insensitive) as well as Markdown. This requires a licence, the same gate as reviewing a Markdown document.

```sh
galley edit page.html
```

When you open an HTML file, galley splits it into two parts. The **content** — prose the markdown document model can carry: paragraphs, headings, lists, tables, blockquotes, and `<pre>` code blocks — becomes `.galley/pages/<base>/content.md`. The **shell** — navigation, script and style elements, `<svg>`, `<img>`, and other elements galley cannot losslessly express as markdown — stays in the template. The editor opens on `content.md`, and that is the document you and the agent read and revise. After every projection galley pours `content.md` back into the template and writes the result to `page.html`. To every other surface this is a normal review; the seam is invisible.

When the editor opens, a three-state segmented control appears below the top bar: **HTML · Both · Content**, starting in **Both**. **Content** shows the recovered prose in the editor — the text you and the agent read and revise. **HTML** shows the real page rendered live in a browser frame, with its own stylesheets, scripts, and images; the browser draws it at full fidelity, not as a screenshot or a marker approximation. **Both** puts them side by side. Only the Content pane is editable; the HTML pane is a read-only live render that refreshes within about a second and a half of an edit — the server's poll cadence. Markdown documents (`galley edit doc.md`) are unaffected: no toggle and no preview; the editor renders exactly as before.

The working directory `.galley/pages/<base>/` holds three things:

- `content.md` — the editable prose, extracted from the page
- `template.json` — the shell segments and per-block wrappers, regenerated at open and refreshed again after every round
- `original.html` — the page as it was the first time you opened it, never overwritten; this is your round-zero cover

### Shell stand-ins

Every shell region leaves a `⟦ shell N ⟧` marker in `content.md` marking where the shell sits. The content pane hides those markers — they are inert, take no visible height, and the caret skips them. The live HTML preview already shows every shell region at full fidelity. The markers remain in `content.md` as structural anchors galley routes on; the shell ships regardless, and the agent is told to leave them in place.

### Navigation and control chrome

Navigation and page furniture never enter the content pane. A `<nav>`, a `<footer>`, and any top control bar — a brand-and-menu strip, a theme toggle, the kind of bar that carries a menu or a button but no prose — are treated as shell: shown at full fidelity in the HTML preview, represented in `content.md` only by a hidden `⟦ shell N ⟧` marker. They are navigation, not review copy, so you do not edit them here. The page's real content — its hero, headings, paragraphs, tables, and code — is what the content pane holds. (A `<header>` that carries the page's lede is content, not chrome; only navigation landmarks and prose-free control strips are held back.)

### Deleting a stand-in

Stand-ins are visual, not structural. If you delete a stand-in, galley does not refuse — the content that followed it merges forward into the previous content region. The orphaned slot renders empty in the page. Use this to collapse two regions of prose that were split by a shell element you no longer want to separate.

### Blocks the page cannot carry

A page holds prose. If `content.md` contains a block the page cannot pour back — a display-math block or front matter typed by mistake — galley refuses the projection with a message of the form:

> htmlpage: a mathBlock block cannot be poured back into this page — pages carry prose, not mathBlock

This routes through the `cannot` machinery and appears in the reviewer's history. Galley dedupes the refusal: while you are typing through an invalid intermediate state, only the first occurrence cuts a round; a successful render clears the memo so the next distinct refusal is reported.

### Out-of-band edits to the page — structural rounds

The template is not frozen at open: galley re-extracts `page.html` every round, so the page's structure can change while a review is in progress. If you or the agent edit `page.html` directly — remove a section, reorder one, add one, change layout — galley picks that up at the next projection. In the normal loop that is the round boundary, and the reload lands where it is safe. If page.html is hand-edited while you are working, the reload can land mid-session: your prose is preserved, and an instruction you have filed and not yet sent is kept. Its place went with the replaced document, so it shows as an unplaced card, and it is still sent with the round. It re-extracts the edited page, refreshes the template, and if the recovered prose differs from what was on the editor, **reloads your editor** to match: the content pane now shows the re-derived markdown for the new structure. This looks and feels like restoring a version — the caret returns to the start and local undo history is cleared.

You still only ever edit Markdown. Structural changes are the agent's job, made directly against the HTML; a content-only round (the agent only changes wording in `content.md`) never triggers a reload — nothing about the structure moved, so the editor stays exactly as you left it.

One edge worth knowing: a single round that both changes the wording of a section (in `content.md`) *and* restructures the page (in `page.html`) has two possible outcomes, depending on whether the restructure touched that section's slot. If the restructure removes the very slot the words needed, galley refuses to render the merge: the page keeps the restructure, the document keeps the words, unmerged, and it says so loudly in the server log — nothing is poured back. If the slot survives, the words can land in a different section than the one they were written for. Prefer changing a section's wording *or* restructuring it in one round, not both.

### What the agent may touch

The agent edits `content.md` for wording, and the page's `.html` directly for structure — removing, reordering, or adding a section, or changing layout. galley re-extracts on the next projection either way, so an agent edit to the page flows back into `content.md` and the shell markers as ordinary content. The agent prompt makes this explicit, and states the ordering: content edits render into the page first, then structural edits to the page are what get re-extracted. Because re-extraction runs every round, `⟦ shell N ⟧` markers renumber each round — the agent (and you, if you are reading the raw markdown) should answer the current markers, not ones remembered from an earlier round.

### Honest limits

The regenerated page keeps your shell and classes; it does not keep your whitespace.

Galley recovers more prose than you might expect. Prose wrapped in styling `<span>`s comes back editable — the span's text is kept, the span element itself is dropped on re-render, so any inline styling it carried is not preserved. Prose inside generic block containers (`<div>`, `<section>`, and others) also becomes editable when the container holds only inline content. Navigation and prose-free control bars are the exception — see *Navigation and control chrome* above. A known consequence: a `<div>` that uses child spans for a two-column label/value layout recovers its words but re-renders single-column — the per-cell styling is not preserved by this pass. Genuine visual elements (`<svg>`, `<img>`, `<canvas>`, `<video>`, `<picture>`) still stay shell. `<pre>` blocks — terminal transcripts, install snippets — are now recovered as editable fenced code that renders back to `<pre>`; their nested syntax `<span>` coloring is dropped on re-render (code text is kept, per-token colors are not — same tradeoff as the prose-span flattening already shipped).

Nothing is photographed. Shell regions — empty wrappers, nav, graphics — are marked by `⟦ shell N ⟧` anchors in `content.md`; those anchors are hidden in the content pane. To see what the page actually looks like, use the live HTML preview pane.
