# Changelog

Newest first. Notes for releases up to 0.10.2 are on the GitHub releases page: https://github.com/schuettc/galley/releases

## Unreleased

- **One button for every block, always visible.** Every top-level heading, code block, table, image, diagram, display-math block and front-matter block now has one square button in the left margin, level with its first line. A block nested inside a list or a quotation has none. It shows without hovering, and Tab reaches it from the keyboard after the document. Pressing it opens the instruction box on that block straight away, outlines what the instruction covers (a heading's whole section, or the block), and leaves your selection alone.
- **Tables, display math and front matter can take an instruction.** Before, the page offered no way to instruct a whole table. A table's card is headed with its column names (`on table: key, value`) instead of a line of markdown.
- **A figure's region is reached from its button.** Pressing an image's or diagram's button opens an instruction on the whole figure, and **Mark a region** in that box lets you drag a rectangle to narrow it to part of the picture.
- A block that already carries instructions shows how many on its button (`1`, `2`, up to `9+`).
- Selecting inside a top-level table, fence, equation or front matter now says you can press the button to its left to instruct the whole block.
- The button is small and faint until you point at it or tab to it, or until it shows a count. A heading's section is outlined as one region, from the heading to the section's last block.
- **An instruction box opens directly beneath its block or selection.** If there is not room for it there, the page scrolls just far enough instead of the box being placed somewhere else; it goes above only for words near the bottom of the window. As you type, it grows only into the room the window has.
- **A click off an empty instruction box closes it.** A box with something typed in it stays open until you send it or cancel.
- The buttons are hidden, and cannot be pressed, after approval, while the agent holds the document, in History and in a page's HTML view.
- **Removed:** the `§` button that appeared beside a heading on hover, the `{}` button beside a code block, and the `⊕ comment on a region` button on a figure. The one block button replaces all three.

Fixed:

- The **Add instruction** button sits next to the selection again. In 0.11.0 it could land at the foot of the window, far from the words.
- A new instruction on a block was briefly listed without its place, so for a moment it could show as unplaced.
- Pressing Enter in a block's box before galley had seen the block filed an instruction on nothing. It now does nothing until the block is known, and the box can be sent, with what you typed kept, as soon as galley sees the block.

## 0.11.0

Every comment is now stored the same way, whatever it is on: selected text (including a selection across paragraphs), a section, a block, a figure or a rectangle on one, a table cell, a code block, or the whole document.

- **Comments survive a galley restart.** Until you press Revise, every comment's words are kept in `.galley/versions/<doc>/pending.json`, which is written before the `.md` on every add, edit and delete. Stopping galley, or a crash, loses none of them: the next `galley edit` of the same `.md`, or of the same `page.html` while the page has not changed outside galley, puts each one back in place. One whose place is gone, including every comment on a `page.html` that was edited outside galley in between, shows as unplaced instead of being lost. Revise sends them as the round and empties the file.
- `galley edit` refuses to start on a `pending.json` written by a newer galley, and leaves it untouched. An unreadable `pending.json` is moved aside to `pending.json.unreadable` and galley starts with no unsent comments.
- **The `.md` carries only ID marks.** While a comment is unsent, the file holds a short mark at its place and never its words: `{>>@comment cm-…<<}` after the highlighted words, a `{>>@comment cb-…<<}` line after a block, and nothing for a whole-document comment. galley links a comment to its place by that ID alone, never by matching words. Revise removes the marks, so versions stay clean.
- **Line breaks work in every comment.** A comment on a section, block or figure may now hold line breaks and blank lines; galley used to refuse them. Every comment box keeps them (Enter files, Shift-Enter breaks a line, including when editing a card), every card shows them, and they reach the agent, `galley round`, `galley pending`, the decision log and History.
- The three comment boxes (on a selection, on the whole document, and a card's edit box) share one size and one behaviour.
- **`galley serve` and `galley comments` are removed**, together with the `<page>.comments.json` file and the in-page review client they used. Review an HTML page with `galley edit page.html`.
- **A hand-typed `{>>note<<}` is no longer imported as a comment.** One inside a sentence is dropped from the file at the next save. One on a line of its own is shown but never sent, and is removed when you press Revise.
- A comment with no words, or on a selection with no words in it, is refused.
- Deleting all the words a comment on selected text was about takes the comment back; undo brings it back. A taken-back comment stays taken back across a page reload and a restart, and is never sent.

**Upgrade note:** send a review in progress on 0.10.4 (press Revise) before upgrading. Its unsent comments on selected text are not carried over, and its old section comments become notes that are shown but never sent.

Developer note: the `just wasm`, `wasm-exec` and `serve` recipes are gone.

Fixed:

- Comments on selected text, and the rectangles on figure comments, were lost if galley stopped before Revise.
- Editing a section comment to add a line break wrote the comment into the document as ordinary prose.
- Changing a word inside a highlighted passage hid its comment from the rail.
- Revert on a hand edit removed every pending comment mark from the document. It now restores only the words that changed and leaves every comment mark in place.
- The reviewer's list and the agent's list were built by two different functions and could disagree about which instructions existed. One builder now serves the rail, `galley wait`, `galley pending` and the round.
- `galley pending`, run with no editor open, left out comments on selected text.
- The whole-document comment box turned line breaks into spaces before sending, and no card showed line breaks.
- Page mode's check for whether the page changed did not ignore comment marks, although it said it did.
- When page mode reloaded the editor because `page.html` was edited by hand, unsent comments went with the replaced document. They are now kept, shown as unplaced, and sent with the round.
- An edit to a comment that had been sent or deleted in the meantime was lost. The words now move to the whole-document box, with the reason, so you can file them again.
- After a section or code-block grip's comment was sent or cancelled, the grip's selection stayed, and the next keystroke replaced the whole section.
- A code block's comment left the caret inside the read-only fence.
- The comment box was placed for the height it opened at, so as it grew it ran off a short window or down over the passage.
- Sending a round could remove a table cell, or a whole blockquote, whose only content was a note.
- Restarting `galley edit page.html` put every unsent comment back unplaced, because the page's prose was extracted afresh from `page.html`, which carries no comment marks. The prose galley kept is now reused while the page has not changed outside galley.
- `galley pending page.html`, run with no editor open, read the page instead of the prose and unsent comments galley keeps for it.

## 0.10.4

- The channel no longer prints a line for a document another live session owns. That refusal is the ownership rule working, but the pi channels harness shows channel stderr as a notification, so sessions under an overlapping scope announced each other's reviews and it read like cross-session bleed. The reason is still in `galley_channel_status`; a review whose owning session has ended is still announced.

## 0.10.3

- Releases are built through the family release actions (tools-actions); the assets, signing and `/dl` paths are unchanged.
- Checked by the family lint set (tools-actions v0.6.0), which replaces galley's own `.golangci.yml`. The findings are fixed (dead loop-variable copies, response bodies closed in tests, a switch for an if-chain, two unused parameters and results) or carry their reason inline. No behaviour change.
- `just verify` is the family push gate: `prepare` (the wasm client), the family Go gate at the version CI pins, and `verify-extra` (the TypeScript gate and the committed editor bundle). The pre-push hook and CI run exactly this. The browser gates are `just verify-slow`, and `just verify-all` runs everything; CI still requires the gates job.
- The pre-commit hook checks formatting instead of rewriting files. Run `just hooks` once per clone.
