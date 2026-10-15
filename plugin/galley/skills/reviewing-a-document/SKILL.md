---
name: reviewing-a-document
description: Use when a human wants to review, edit, or revise a Markdown or HTML document with you — a draft, a blog post, a spec, a README, a web page — or says "review this in galley", "open this in galley", "let's edit this together", or asks you to open a document for review. Covers starting a galley review, answering the rounds it sends, and finishing it.
---

# Reviewing a document in galley

galley is a document two parties revise in rounds. The reviewer reads a draft in the browser, edits anything easier to fix by hand, and leaves **instructions** on the passages that need you. Pressing **Revise** sends that round. You revise the document — your changes land, they are not proposals — and the reviewer reads what changed. Every round is a version. There is no accept/reject.

## Starting a review

**A review does not exist until an editor is running, and you start one with the `galley_open` tool.** Call it with the document's path. It starts the editor for this session, in the background, and opens the page in the human's browser. From then on their Revise rounds arrive here as channel events. It returns the URL too: give it to the human if they closed the tab. Calling it again for the same document returns the same URL and opens nothing, unless another session owns it.

`galley_open` accepts HTML files too. For `page.html` it extracts the prose into `.galley/pages/<base>/content.md`, opens the editor on that file, and re-renders the page after every round. The agent edits `content.md` — not `page.html` directly.

**Never run `galley edit` from a shell while the channel is present.** An editor opened that way belongs to no session, so any channel whose scope covers it may claim it, and the rounds can land in a different conversation. `galley edit` is the human's command, for a review with no agent attached.

Do **not** poll `galley wait` to find out whether a review started. If no editor is running for the file, `galley wait` exits immediately — that means "nothing to wait for", not "try again". Call `galley_open` instead.

Between rounds, galley loads your edits to an open document. If you and the reviewer change it at the same time, the reviewer's copy is kept and yours is saved in .galley/recovery. Stop the review before moving or deleting the file.

## What the reviewer does, so you can tell them

**They will ask you this, and the answer is not in the browser.** galley opens onto a document with no instructions, and every gesture below is discoverable only by trying it. The page teaches two sentences in an empty rail — *"Select any words in the document to ask for a change. Your instructions collect here, then go to the agent as one round. Or click anywhere in this panel to ask about the whole document."* — and that is the whole of the onboarding. Say the rest when they ask, and use these words, because they are the product's own.

**Asking for a change.** Select the words and an instruction button appears beside them — the instruction hangs on that passage. For the whole document, click blank space in the right-hand panel, or right-click with nothing selected anywhere below the bar (the text, the margins, the space under the document, the panel) and choose `Instruction on the whole document`. A right-click on selected text is the browser's own menu, with its Copy. There is no mode to switch: a selection means the passage, no selection means the whole document.

**Editing by hand.** Their typing applies directly. There is nothing to accept and nothing to propose — if a word is easier to fix than to explain, they fix it. Each hand edit collects in the rail beside their instructions, with a `revert` on it if they change their mind.

**The rail is one list.** Instructions and hand edits both, in the right-hand column: everything that will reach you on the next round, and nothing else. It is not a history — `History` in the bar is, and every round is a version there with its own diff.

**Sending.** `Revise` sends the round to you. `Revise & Approve` sends it and approves in the same press, for the last few nits they trust you to land without another look. `Approve` alone ends the review with the document as it stands.

**Approving in the conversation.** If the reviewer tells you here that they approve the document, call the `galley_approve` tool with its path. It ends the review exactly as their `Approve` button would, and the page locks itself. Call it only on their word; never approve on their behalf because the revision looks done.

**Then it arrives here.** They do not have to tell you they pressed it — a round reaches this session on its own.

## Answering a round

A round arrives as `<channel source="galley" doc="…" reason="revise|settle|changed|approve|closed">`. The notification carries the instructions and their quoted anchors. **Use that captured round.** Do not re-read `galley pending` for it — a sent round is no longer pending, and `pending` will show you only newer unsent work.

1. If the work will take more than a moment, ack `working` first so the reviewer is not staring at silence.
2. **Edit the `.md` file directly with your normal file tools.** While you hold the round, galley watches the file and streams every save into the reviewer's browser — so save as often as you like. They are watching it happen.

   **Make targeted edits. Never rewrite the whole file.** The reviewer is editing that same file at the same time, so your copy is stale the moment you read it. One whole-file write silently destroys everything they typed since — including passages they deliberately deleted, which come back because they are still in your copy.

   This has happened and it cost a reviewer a section: a 21-instruction round answered with a single write of the entire document, and a paragraph cut in the previous round reappeared inside an 8k expansion. If a write is rejected with *"file has been modified since read"*, that guard is firing correctly — the reviewer changed the document under you. Re-reading one line to satisfy the tool and writing the whole file anyway defeats the guard rather than answering it.
3. When the whole revision is in the file, call `galley_ack` **once**, `state: "answered"`, with a one-sentence note. Once for the whole revision, never per save.

Pass `changes` with that ack: one entry per change, each quoting **a few words you actually wrote** so galley can locate it, and naming in `answers` which instruction it discharges, by the key the round handed you. A key is printed beside the instruction it belongs to, and that printed form is what you copy:

```
> Cognito mints every token  [key cm-0fe440429378b2b0]
spell this out — it reads as a code identifier
``` galley computes the real change list from the two documents; your entries only annotate it. An entry it cannot place is ignored silently — no error, the change still reaches the reviewer, just without your sentence beside it.

### Markdown only, and a narrow dialect

Ordinary Markdown expresses everything you need: formatting, links, headings, lists, tables. **Do not write** raw HTML, footnotes, reference-style links, autolinks, definition lists, `:::` directives, or `[!NOTE]` callouts. galley refuses those and holds the save until a later one parses — so a stray `<br>` makes the reviewer's page go quiet with no visible error.

### If you lose the round

A round is delivered once. If your context was compacted, or this session restarted mid-review, read it again:

```
galley round path/to/doc.md
```

It prints the same instructions with the same keys you were given. **Do not reach for `galley pending` instead** — a sent round is no longer pending, so pending answers a different question, and answering the wrong one is how a reviewer loses work.

### When you cannot do something

```
galley cannot path/to/doc.md --why "what stopped you"
```

That records an unchanged exception round. It is a **report, not an argument** — do not use it to push back on an instruction you simply disagree with, and do not retry into it. Answering seven of eight instructions and stating the eighth as an open question in the document is usually better than refusing the round.

### An instruction is not a conversation

A comment is discharged **by the revision itself**. Do not reply to instructions, explain your reasoning at length, or ask the reviewer to approve your approach. If a revision misses, the reviewer answers with a different instruction in the next round. That is the design — "try again" is not a useful signal, so galley does not have one.

## The other events

- **`approve`** — the reviewer approved the document as it stands. The review is over.
- **`changed`** — re-read the document before doing anything else.
- **`closed`** — the editor is gone; nobody is waiting.
- **Silence when you expect a round** — call `galley_channel_status`. It lists which open documents this session is attached to, and why any others are not.

## Sharing an approved document

After approval, the slot Revise leaves holds a filled `Share ▾` button. Its menu, in order:

- **`Download HTML`** renders the document in the project's theme and the browser downloads it as `<name>.html`. The file is self-contained: fonts, logos, Mermaid diagrams and images beside the document are inlined, so it opens offline; an image by `http(s)` address stays a link.
- **`Download Markdown`** downloads the `.md` as it is on disk, as `<name>.md`.
- **`Share HTML`** writes the same page to `<name>.html` beside `<name>.md`, replacing an earlier one, and puts that file on the clipboard. Pasting into Slack or an email attaches it. The status line says `<name>.html copied · paste to attach`.
- **`Share Markdown`** puts the `.md` file on the clipboard the same way. The status line says `<name>.md copied · paste to attach`.

The two Share items appear only on macOS. If the clipboard refuses the file, the status line shows the error.

An HTML page under review offers `Download HTML` and `Share HTML`, both for the page itself, and no Markdown items.

**The preview.** For a Markdown review, `galley_open` also returns `preview`, the address of the same branded page served live. It reloads after every save of the document, by the reviewer or by you. Give it to the reviewer when they want to see the shared look while editing; the `HTML preview ↗` button in the bar opens it too. A page review has no preview address.

## Making a theme

A theme changes only the look of the saved HTML and the preview: colours, fonts, logo, label and an optional extra stylesheet. Every theme uses galley's one layout. Themes live in the human's config, not in a repository, so one theme serves every repository of a brand:

```
~/.config/galley/themes/<name>/
  theme.json
  logo-light.svg     the logo drawn on a light page: colours meant for a light background
  logo-dark.svg      the logo drawn on a dark page
  extra.css          optional, loaded after galley's styles
  <font files>       any that theme.json names
```

(`$XDG_CONFIG_HOME/galley/themes/` when that is set.) Every field of `theme.json` is optional; anything left out comes from galley's base theme:

```json
{
  "label": "Acme",
  "light": { "bg": "#ffffff", "fg": "#1a1a2e", "muted": "#5c5c70", "line": "#e4e4ec", "accent": "#6b4eff" },
  "dark":  { "bg": "#121218", "fg": "#ececf4", "muted": "#9a9ab0", "line": "#2c2c3a", "accent": "#a594ff" },
  "fonts": [
    { "family": "Aeonik", "file": "Aeonik-Regular.woff2", "weight": "400" },
    { "family": "Aeonik", "file": "Aeonik-Bold.woff2", "weight": "700", "style": "normal" }
  ]
}
```

- **The default page** is galley's one layout for every theme: an "On this page" sidebar of the `##` and `###` headings on a hairline rail, `#` and `##` headings in a strong accent with a rule above each `##`, inline code on a wash, code blocks, quotes and diagrams on panel cards, and tables with a small uppercase monospace header and striped rows. A wide table scrolls.
- **The header** appears only when the theme has a logo or a `label`. The base theme has neither, so its page starts with the sidebar and the document.
- **Logo.** Shown at the left of the sticky header at 1.6rem high (about 26px), width following the SVG's aspect ratio. A `viewBox` is enough; `width`/`height` attributes are not needed. The page shows `logo-dark.svg` in dark mode and `logo-light.svg` otherwise. With no `logo-light.svg` there is no logo. With `logo-light.svg` but no `logo-dark.svg`, the light logo shows in dark mode too, so ship a dark one if the light one is unreadable on a dark page.
- **`label`** is plain text in the header, in `fg` at 14px and weight 600, after a hairline divider when there is a logo. Left out or empty, there is no label.
- **`light` and `dark`** are the palettes for each system colour scheme. galley mixes four more colours from them, so a theme supplies only these five:
  - `bg`: the page and the header background.
  - `fg`: body text, code-block text and the label.
  - `muted`: the sidebar links and its caption, and the table header text.
  - `line`: the header's bottom border, the rule above each `##` heading, `---` rules, table borders, card borders and the sidebar rail.
  - `accent`: the blockquote bar and a hovered sidebar link's bar.
  - Derived: `--accent-strong` (65% `accent` into `fg`) colours links, `#` and `##` headings, inline code text and a hovered sidebar link. `--accent-wash` (10% `accent` into `bg`) is the background of inline code. `--panel` (35% `line` into `bg`) is the card behind code blocks, quotes and diagrams, the table header, and every other table row. `--panel-raised` (12% `accent` into `bg`) fills diagram nodes. Set any of them in `extra.css` to pin it.
- **Mermaid diagrams** use Mermaid's `base` theme coloured from the same palette, switching with the scheme, on a `--panel` card in the monospace font. Nodes and sequence actors are filled with `--panel-raised` and subgraphs with `--panel`. Node and actor borders are `accent`. All diagram text is `fg`, including sequence messages. Edges are `muted`. Edge-label backgrounds are `bg`. Notes and the secondary colour are `line`.
- **`fonts`** lists font files in the theme folder to embed. Each needs a `family` and a `file`; `weight` and `style` default to `normal`. The families become the body font, ahead of the system stack. Code always uses a monospace stack. A listed file that is missing is an error naming it.
- **`extra.css`** can target these stable elements:
  - `header.g-head`, when there is one, containing `picture.g-logo` (its `img`) and `span.g-label`, each only when the theme has it.
  - `div.g-wrap`, the centred row of sidebar and document.
  - `nav.g-side`, the sidebar. It lists the `##` and `###` headings as `a.g-h2` and `a.g-h3`, and is hidden under 52rem wide.
  - `main.g-body`, the document. The first `#` heading is the page title. Diagrams are `pre.mermaid`.
  - The colours are the custom properties `--bg`, `--fg`, `--muted`, `--line` and `--accent`, the derived `--accent-strong`, `--accent-wash`, `--panel` and `--panel-raised`, and the fonts are `--sans` and `--mono`.

A project chooses its theme in `.galley/config.json`, either in the document's folder or in any folder above it. The nearest one wins:

```json
{ "theme": "acme" }
```

With no config file, or no `theme` in it, the base theme is used. A theme name that does not exist, a `theme.json` that does not parse, or a missing font file makes the preview, `Download HTML` and `Share HTML` show the error instead of a page.

**Check a theme with `galley render <test doc.md>`.** It writes the page Share HTML writes, beside the document (`-o` writes it elsewhere), and prints its path. A theme error is printed instead, exits non-zero and writes nothing. Open the file in light and dark mode. The test document should have headings at levels 2 and 3, a table, inline code, a code block and a Mermaid diagram. While reviewing, the preview (the `preview` address `galley_open` returned) shows the same page as the document changes.

## Without the channel

The channel is a research preview: it needs Anthropic authentication and is **unavailable on Bedrock, Vertex and Foundry**, and it only exists in a session launched with the channel flag. Every other agent — a different harness, a script, CI — uses the pull form instead:

```
galley wait path/to/doc.md
```

It blocks until the reviewer sends a round, prints the captured instructions and their anchors, and exits when the review ends. The loop is otherwise identical: edit the file, then `galley ack <doc> --state answered --note "…"`. `galley agent-prompt <doc>` prints the full protocol for an agent starting cold.

## Licensing

Every command is free to run; galley never gates a feature and never contacts a server. It is free for any organization under 25 people, and larger organizations buy a one-time license by headcount at https://galley.tools/buy — the purchase receipt is the proof of compliance, and there is nothing to install or activate. If `galley` is not on PATH, the human needs to install the binary; this plugin ships the protocol, not the program.
