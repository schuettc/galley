# Changelog

Newest first. Notes for releases up to 0.10.2 are on the GitHub releases page: https://github.com/schuettc/galley/releases

## 0.10.4

- The channel no longer prints a line for a document another live session owns. That refusal is the ownership rule working, but the pi channels harness shows channel stderr as a notification, so sessions under an overlapping scope announced each other's reviews and it read like cross-session bleed. The reason is still in `galley_channel_status`; a review whose owning session has ended is still announced.

## 0.10.3

- Releases are built through the family release actions (tools-actions); the assets, signing and `/dl` paths are unchanged.
- Checked by the family lint set (tools-actions v0.6.0), which replaces galley's own `.golangci.yml`. The findings are fixed (dead loop-variable copies, response bodies closed in tests, a switch for an if-chain, two unused parameters and results) or carry their reason inline. No behaviour change.
- `just verify` is the family push gate: `prepare` (the wasm client), the family Go gate at the version CI pins, and `verify-extra` (the TypeScript gate and the committed editor bundle). The pre-push hook and CI run exactly this. The browser gates are `just verify-slow`, and `just verify-all` runs everything; CI still requires the gates job.
- The pre-commit hook checks formatting instead of rewriting files. Run `just hooks` once per clone.
