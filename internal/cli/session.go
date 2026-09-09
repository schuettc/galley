package cli

import "os"

// sessionID is the id of the session THIS PROCESS SERVES, and only `galley
// channel` reads it. The channel is spawned by a harness that names the
// session in the child's environment, one variable per harness: Claude Code
// sets CLAUDE_CODE_SESSION_ID for its MCP servers; channels.tools (pi) sets
// AGENT_SESSION_ID for the servers it spawns, and deliberately not the Claude
// one, because muster reads that to decide whether a session is Claude Code.
// A harness sets exactly one, so this is a per-harness map, not a fallback
// chain. An empty answer means the channel has no session and attaches to
// unowned documents only.
//
// `galley edit` does NOT read this. An editor's owner is --owner or nothing —
// see requireLiveOwner for why the environment stopped being trusted there.
func sessionID() string {
	if id := os.Getenv("CLAUDE_CODE_SESSION_ID"); id != "" {
		return id
	}
	return os.Getenv("AGENT_SESSION_ID")
}
