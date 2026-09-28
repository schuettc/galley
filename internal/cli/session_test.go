package cli

import "testing"

// The channel serves the session tools-common/harness resolves: a process its
// parent marked as its own (AGENT_SESSION_CHILD=1, pi-claude-bridge's Claude
// children) serves the parent's AGENT_SESSION_ID; otherwise the Claude id
// wins, so a Claude session started from inside pi keeps its own identity.
// All three variables are pinned per case so the test never reads the
// environment it happens to run in. `galley edit` reads none of them.
func TestSessionID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claude string
		agent  string
		child  string
		want   string
	}{
		{"neither set", "", "", "", ""},
		{"claude only", "claude-abc", "", "", "claude-abc"},
		{"agent only", "", "agent-xyz", "", "agent-xyz"},
		{"claude launched from pi: both ids, no marker, claude wins", "claude-abc", "agent-xyz", "", "claude-abc"},
		{"bridge child: both ids and the marker, agent wins", "claude-abc", "agent-xyz", "1", "agent-xyz"},
		{"marker without an agent id is ignored", "claude-abc", "", "1", "claude-abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE_SESSION_ID", tc.claude)
			t.Setenv("AGENT_SESSION_ID", tc.agent)
			t.Setenv("AGENT_SESSION_CHILD", tc.child)
			if got := sessionID(); got != tc.want {
				t.Errorf("sessionID() = %q, want %q", got, tc.want)
			}
		})
	}
}
