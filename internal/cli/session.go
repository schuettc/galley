package cli

import "github.com/schuettc/tools-common/harness"

// sessionID is the id of the session THIS PROCESS SERVES, and only `galley
// channel` reads it. The channel is spawned by a harness that names the
// session in the child's environment: Claude Code sets CLAUDE_CODE_SESSION_ID
// for its MCP servers; channels.tools (pi) sets AGENT_SESSION_ID. When both
// are present the family rule in tools-common/harness decides: a process a
// parent marked as its own (AGENT_SESSION_CHILD=1, set by pi-claude-bridge on
// the Claude Code it runs for a pi turn) serves the parent's AGENT_SESSION_ID;
// otherwise the Claude id wins, because a Claude session started from inside
// pi is its own session. An empty answer means the channel has no session and
// attaches to unowned documents only.
//
// `galley edit` does NOT read this. An editor's owner is --owner or nothing —
// see requireLiveOwner for why the environment stopped being trusted there.
func sessionID() string {
	return harness.FromEnv().SessionID
}
