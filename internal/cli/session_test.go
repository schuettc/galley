package cli

import "testing"

// The channel reads its session id from the variable its spawner set: Claude
// Code sets CLAUDE_CODE_SESSION_ID, channels.tools sets AGENT_SESSION_ID. Each
// harness sets exactly one; the Claude variable wins if both are ever present
// so a Claude Code session never changes identity because something else was
// also in its environment. `galley edit` no longer reads either.
func TestSessionID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claude string
		agent  string
		want   string
	}{
		{"neither set", "", "", ""},
		{"claude only", "claude-abc", "", "claude-abc"},
		{"agent only", "", "agent-xyz", "agent-xyz"},
		{"both set, claude wins", "claude-abc", "agent-xyz", "claude-abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE_SESSION_ID", tc.claude)
			t.Setenv("AGENT_SESSION_ID", tc.agent)
			if got := sessionID(); got != tc.want {
				t.Errorf("sessionID() = %q, want %q", got, tc.want)
			}
		})
	}
}
