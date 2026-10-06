# galley

- The reviewer works in the browser and the agent does everything else, so the plugin skill `plugin/galley/skills/reviewing-a-document/SKILL.md` is galley's interface. Any change to the protocol or a capability updates that skill in the same change; `internal/cli/pluginskill_test.go` checks it. The skill teaches the reviewer's gestures too, since the reviewer learns them from the agent.
- The code is written around these ygo v1.43.0 traps: never read inside `Transact` (it deadlocks silently, `YMap.Get` included); attached `ForEach` skips nested shared types, so use `Keys()` + `Get`; `YText` indexes by UTF-16 code units, not runes; server writes go through the websocket `Apply`, not `doc.Transact`, or peers never see them.
