// schemacorpus.mjs — EVERY BLOCK KIND IN EVERY CONTAINER, AT EVERY POSITION
// THAT CHANGES THE ANSWER.
//
// The corpus exists because of HOW the three schema bugs survived. Every .md in
// this repository passes; FuzzRoundTrip is Parse<->Serialize only, and all
// eighteen spellings of the list-item bug round-trip byte-identically through
// it; schema_drift_test.go and probe.mjs's FRAGMENT_NODES check node NAMES, not
// content rules. A corpus of realistic documents is a corpus of the shapes
// somebody already thought of — which is precisely the population these bugs
// were not in.
//
// So this is generated rather than written: the cross product of every
// construct markdown can spell against every container it can sit in, at each
// position that can change the verdict. `bullet-later` is not `bullet-first`
// (listItem is `paragraph block*`, so only the FIRST child has to be a
// paragraph); `bullet-solo` is not `bullet-first` (a list with one item is what
// makes the emptied-list cascade reachable); and `quote-bullet` is not
// `bullet-bullet` (the cascade needs a blockquote at the top of it).
//
// THE HEAD AND TAIL PARAGRAPHS ARE PART OF THE FIXTURE. A document whose only
// block is the construct under test cannot show a cascade reaching the top
// level, and cannot show the loss being SURVIVABLE-LOOKING — which is the whole
// harm in `1. done`.

const HEAD = 'A settled opening paragraph that nothing in this file may touch.';
const TAIL =
  'A closing paragraph that must still be here, word for word, after.';

// Each construct as the unindented lines it occupies.
export const CONSTRUCTS = {
  paragraph: ['ordinary prose in the slot'],
  heading1: ['# a heading in the slot'],
  heading3: ['### a deeper heading in the slot'],
  emptyHeading: ['#'],
  // S4: a heading whose ENTIRE content is a comment. `heading` is `inline*`, so
  // nothing is deleted and this gate reads green on it — it is here because the
  // corpus is also what the serializer's byte-identity is measured over, and
  // this is the one shape in the family whose bytes the fix deliberately
  // changes. See extractCriticBlocks.
  noteHeading: ['# {>>write the title here<<}'],
  fence: ['```js', 'const inSlot = 1;', '```'],
  fenceNoLang: ['```', 'plain fenced text', '```'],
  emptyFence: ['```', '```'],
  indentedCode: ['    four space indented code'],
  // Display math — docmodel.MathBlock, carried verbatim. It is here for the
  // fence's reason: a raw-text block reaches the fragment as one unmarked run,
  // and every slot is a container whose content rule it has to satisfy.
  math: ['$$', 'E = mc^2', '$$'],
  // AND ONE WITH NO BODY, which is the fence's `emptyFence` for this construct:
  // `content: 'text*'` accepts a node with no child, and `text*` is what makes
  // that legal where `block+` would not — the same distinction the blank table
  // cell was lost to.
  emptyMath: ['$$', '$$'],
  blockquote: ['> quoted words in the slot'],
  emptyBlockquote: ['>'],
  bulletList: ['- a bullet in the slot'],
  orderedList: ['1. an ordered item in the slot'],
  emptyBullet: ['-'],
  emptyOrdered: ['1.'],
  rule: ['***'],
  ruleDash: ['---'],
  image: ['![diagram alt](pic.png)'],
  table: ['| h1 | h2 |', '| --- | --- |', '| a | b |'],
  blankCellTable: ['| h1 | h2 | h3 |', '| --- | --- | --- |', '| a |  | c |'],
  noteBlock: ['{>>a block note in the slot<<}'],
  noteDoc: ['{>>@document a document note in the slot<<}'],
  twoNotes: ['{>>note one<<} {>>note two<<}'],
  // A BLOCK COMMENT'S ID MARK: <note anchor id> with NO text run, because the
  // words live in the unsent round. The slots put it after a paragraph, in a
  // list item and in a table cell; noteWithIdAfterFence puts it after a fence.
  noteWithId: ['{>>@comment cb-0123456789abcdef<<}'],
  noteWithIdAfterFence: [
    '```js',
    'const inSlot = 1;',
    '```',
    '',
    '{>>@comment cb-0123456789abcdef<<}',
  ],
  // A TEXT COMMENT'S ID MARK, on its highlight: one piece, a piece crossing
  // emphasis (two pieces, two marks), and two pieces over two paragraphs.
  highlightWithId: ['prose with {==a highlight==}{>>@comment cm-1<<} in it'],
  highlightWithIdAcrossBold: [
    'prose {==that crosses ==}{>>@comment cm-2<<}{==**bold**==}{>>@comment cm-2<<} text',
  ],
  highlightWithIdTwoParagraphs: [
    'the first {==half==}{>>@comment cm-3<<}',
    '',
    '{==and the second==}{>>@comment cm-3<<} half',
  ],
  hardBreak: ['a line with a break\\', 'and its continuation'],
  criticIns: ['prose with {++an insertion++} in it'],
  criticDel: ['prose with {--a deletion--} in it'],
  criticSub: ['prose with {~~brown~>red~~} in it'],
  criticHl: ['prose with {==a highlight==} in it'],
  emphasis: ['prose with **bold** and *italic* and `code` in it'],
  link: ['prose with [a link](https://example.com) in it'],
};

const indent = (lines, pad) => lines.map((l) => (l === '' ? '' : pad + l));

/** rows returns the whole corpus as {id, src} objects. */
export function rows() {
  const out = [];
  const emit = (id, src) => out.push({ id, src });

  for (const [name, lines] of Object.entries(CONSTRUCTS)) {
    // The document root.
    emit(`root:${name}`, [HEAD, '', ...lines, '', TAIL, ''].join('\n'));

    // Inside a blockquote, alone and after a leading quoted paragraph.
    emit(
      `quote-only:${name}`,
      [HEAD, '', ...indent(lines, '> '), '', TAIL, ''].join('\n'),
    );
    emit(
      `quote-second:${name}`,
      [
        HEAD,
        '',
        '> a leading quoted paragraph',
        '>',
        ...indent(lines, '> '),
        '',
        TAIL,
        '',
      ].join('\n'),
    );

    // A bullet item's FIRST child, and a LATER child of an item that already
    // opens with a paragraph. Only the first has to be a paragraph, so these
    // two positions have different answers and both are needed.
    emit(
      `bullet-first:${name}`,
      [
        HEAD,
        '',
        '- alpha',
        ...indent(lines, '  ').map((l, i) => (i === 0 ? `- ${l.slice(2)}` : l)),
        '- gamma',
        '',
        TAIL,
        '',
      ].join('\n'),
    );
    emit(
      `bullet-later:${name}`,
      [
        HEAD,
        '',
        '- alpha',
        '- a leading item paragraph',
        '',
        ...indent(lines, '  '),
        '- gamma',
        '',
        TAIL,
        '',
      ].join('\n'),
    );

    // An ordered item's first child — the case whose loss RENUMBERS the
    // survivors, so the file still reads as a complete procedure.
    emit(
      `ordered-first:${name}`,
      [
        HEAD,
        '',
        '1. alpha',
        ...indent(lines, '   ').map((l, i) =>
          i === 0 ? `2. ${l.slice(3)}` : l,
        ),
        '3. gamma',
        '',
        TAIL,
        '',
      ].join('\n'),
    );

    // A table cell. Only inline content is reachable, but the verdict is the
    // corpus's to reach and not the corpus author's to assume.
    emit(
      `cell-middle:${name}`,
      [
        HEAD,
        '',
        '| knob | note | unit |',
        '| --- | --- | --- |',
        `| timeout | ${lines.join(' ').replace(/\|/g, '\\|')} | ms |`,
        '',
        TAIL,
        '',
      ].join('\n'),
    );

    // Two levels of nesting, which is where the cascade can reach a container
    // the reader would never suspect.
    emit(
      `quote-bullet-first:${name}`,
      [
        HEAD,
        '',
        '> - alpha',
        ...indent(lines, '>   ').map((l, i) =>
          i === 0 ? `> - ${l.slice(4)}` : l,
        ),
        '> - gamma',
        '',
        TAIL,
        '',
      ].join('\n'),
    );
    emit(
      `bullet-bullet-first:${name}`,
      [
        HEAD,
        '',
        '- alpha',
        ...indent(lines, '    ').map((l, i) =>
          i === 0 ? `  - ${l.slice(4)}` : l,
        ),
        '- gamma',
        '',
        TAIL,
        '',
      ].join('\n'),
    );

    // A SINGLE-ITEM list, where losing the item empties the list and the
    // cascade takes the list too.
    emit(
      `bullet-solo:${name}`,
      [
        HEAD,
        '',
        ...indent(lines, '  ').map((l, i) => (i === 0 ? `- ${l.slice(2)}` : l)),
        '',
        TAIL,
        '',
      ].join('\n'),
    );
    emit(
      `quote-solo:${name}`,
      [HEAD, '', ...indent(lines, '> '), '', TAIL, ''].join('\n'),
    );
  }

  // THE WORST CONFIRMED CASE, WRITTEN OUT AS SOMEBODY WOULD ACTUALLY WRITE IT.
  // Two steps and both commands gone, and `3. done` renumbered to `1. done`, so
  // the file reads as a complete one-step procedure. Plausible corruption is
  // worse than visible corruption.
  emit(
    'real:install-list',
    [
      '# Install',
      '',
      '1. ```sh',
      '   brew install galley',
      '   ```',
      '2. ```sh',
      '   galley edit doc.md',
      '   ```',
      '3. done',
      '',
      'Read the steps above before running anything.',
      '',
    ].join('\n'),
  );
  // The cascade in full: item, then list, then blockquote. Nothing is left.
  emit(
    'real:quoted-deploy',
    [
      '# Deploy',
      '',
      '> - ```sh',
      '>   ./deploy --now',
      '>   ```',
      '',
      'That is the whole procedure.',
      '',
    ].join('\n'),
  );
  emit(
    'real:checklist',
    [
      '# Release checklist',
      '',
      '- run the tests',
      '- ```sh',
      '  just verify',
      '  ```',
      '- ### Then tag it',
      '- > Do not skip this.',
      '- ![the dashboard](dash.png)',
      '- | knob | value |',
      '  | --- | --- |',
      '  | timeout | 30s |',
      '- {>>who owns this step?<<}',
      '-',
      '- done',
      '',
      'Nothing above may go missing.',
      '',
    ].join('\n'),
  );

  // Degenerate documents. None of these should report a deletion: an empty
  // document is a document nobody has lost anything from, and the top level is
  // deliberately not content-checked (see schemacheck.mjs).
  // FRONT MATTER IS A DOCUMENT-HEAD CONSTRUCT, so it is emitted here rather
  // than through the slots above: it only exists at byte 0 and the cross
  // product would put it inside a blockquote, where it is prose.
  //
  // It is the construct that was neither modelled nor refused. goldmark read
  // the "---" as a thematic break and the keys under it as a SETEXT HEADING,
  // so every open destroyed it — and because nothing was unsupported, the
  // refusal machinery never saw it. These are here so this gate reads the
  // block the browser is actually handed.
  emit(
    'front:yaml',
    '---\ntitle: The Spec\nstatus: draft\n---\n\n# The Spec\n\nProse under it.\n',
  );
  emit(
    'front:toml',
    '+++\ntitle = "The Spec"\ndraft = true\n+++\n\n# The Spec\n',
  );
  emit('front:only', '---\ntitle: The Spec\n---\n');
  emit(
    'front:looks-like-markdown',
    '---\ntitle: "# not a heading"\nlist:\n  - a\n  - b\nbody: |\n  ## nor this\n  *not emphasis*\n---\n\nprose\n',
  );
  emit('front:no-gap', '---\ntitle: The Spec\n---\n# The Spec\n');
  emit(
    'front:with-every-construct-under-it',
    [
      '---',
      'title: The Spec',
      '---',
      '',
      '# Heading',
      '',
      '- a bullet',
      '',
      '| h1 | h2 |',
      '| --- | --- |',
      '| a | b |',
      '',
      '{>>a note<<}',
      '',
      '---',
      '',
      'After a real thematic break.',
      '',
    ].join('\n'),
  );
  // NOT front matter, and each must stay that way: an unterminated opener, an
  // empty block, and a leading thematic break with another one after it — the
  // shape the serializer's own output takes for two leading rules.
  emit('front:unterminated', '---\n\nprose with no closing delimiter\n');
  emit('front:empty', '---\n---\n');
  emit('front:two-leading-rules', '***\n\n***\n');
  emit('front:rule-prose-rule', '***\n\nprose\n\n***\n');

  emit('doc:empty', '');
  emit('doc:blank', '\n\n\n');
  emit('doc:spaces', '   \n');
  emit('doc:only-bare-bullet', '-\n');
  emit('doc:only-empty-quote', '>\n');
  emit('doc:only-note', '{>>a note and nothing else<<}\n');
  emit('doc:only-fence', '```sh\necho hi\n```\n');

  return out;
}
