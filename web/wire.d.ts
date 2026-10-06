// wire.d.ts — THE GO↔JS CONTRACT, GENERATED. DO NOT EDIT.
//
// Written by internal/serve/wire_test.go from the json tags of the five view
// structs that ARE the wire: PendingView and InstructionView
// (internal/serve/editmode.go), roundView, diffView and changeView
// (internal/serve/versions.go), plus every struct they reach — including
// across a package boundary, which is how suggest.BlockRef and review.Region
// get here.
//
// Regenerate with `just wire`. A drift between this file and the Go
// structs fails `just verify`, which is Go-only and therefore runs on every
// machine, in CI and in the pre-push hook, whether or not node is installed.
// That is the whole point of it: the rename of `pending.suggestions` to
// `pending.instructions` broke two browser gates and went unnoticed for
// weeks, because the only things that could see it were scripts nobody ran.
//
// READING THE OPTIONALITY, because it is stated honestly rather than
// conveniently:
//
//   - `x?: T` — the Go field carries `omitempty`, so the key is ABSENT from
//     the payload when it is the zero value. An absent slice is not an empty
//     one and is not null.
//   - `x: T[] | null` — no `omitempty`, so a nil slice is serialized as JSON
//     `null`. The type says so rather than promising an array: `|| []` at the
//     reader is load-bearing, and a declaration that hid this would make the
//     one line protecting against it look like superstition.
//   - a `time.Time` is a string. It is not a shape on the wire.

export interface PendingView {
  instructions: InstructionView[] | null;
  blocks?: BlockRef[];
  changes?: ReviewerChange[];
  changesDropped?: number;
}

export interface InstructionView {
  key: string;
  text: string;
  quote?: string;
  at: string;
  run?: string;
  anchor?: string;
  anchorKey?: string;
  blockKind?: string;
  region?: Region;
}

export interface ReviseStateView {
  running: boolean;
  waiting: boolean;
  sinceMs: number;
  ack: string;
  ackNote: string;
  ackAgoMs: number;
  landed: number;
  cannot: string;
  cannotAgoMs: number;
  handoff: boolean;
  draftError: string;
  diskNotice: string;
  sealed: boolean;
  verdict: string;
  verdictAt: number;
}

export interface VersionsView {
  doc: string;
  rounds: RoundView[] | null;
}

export interface RoundView {
  n: number;
  at: string;
  authors: string;
  reason: string;
  instruction: string;
  answers: number;
  asked: string;
  instructions?: string[];
  askedInstructions?: string[];
  changed: number;
}

export interface DiffView {
  from: number;
  to: number;
  view: string;
  html: string;
  regions: number;
  refused: boolean;
  runs: number;
  changes?: ChangeView[];
}

export interface ChangeView {
  region: number;
  place?: string;
  asks?: string[];
  note?: string;
}

export interface BlockRef {
  key: string;
  kind: string;
  label: string;
  index: number;
}

export interface ReviewerChange {
  key?: string;
  kind: string;
  before?: string;
  after?: string;
}

export interface Region {
  x: number;
  y: number;
  w: number;
  h: number;
}
