# Changelog

Newest first. Notes for releases up to 0.10.2 are on the GitHub releases page: https://github.com/schuettc/galley/releases

## Unreleased

- Checked by the family lint set (tools-actions v0.6.0), which replaces galley's own `.golangci.yml`. The findings are fixed (dead loop-variable copies, response bodies closed in tests, a switch for an if-chain, two unused parameters and results) or carry their reason inline. No behaviour change.
- `just verify` is the family push gate: `prepare` (the wasm client), the family Go gate at the version CI pins, and `verify-extra` (the TypeScript gate and the committed editor bundle). The pre-push hook and CI run exactly this. The browser gates are `just verify-slow`, and `just verify-all` runs everything; CI still requires the gates job.
- The pre-commit hook checks formatting instead of rewriting files. Run `just hooks` once per clone.
