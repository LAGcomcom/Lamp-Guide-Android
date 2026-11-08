//go:build !android

package agents

import "zenui/internal/zen/store"

// builtinAgents is the desktop set: every adapter here targets a program
// installed on the same machine, and writes into that program's own config
// (~/.config/opencode/opencode.json, ~/.codex/config.toml, ~/.dsh/profiles/
// <p>/cordis.patch.yml, …). Those paths do not exist on a phone, so the
// Android build ships a shorter list instead of a list of no-ops.
func builtinAgents(st *store.Store) []Agent {
	return []Agent{
		newZCode(),
		newOpenCode(),
		newCodex(),
		newClaude(),
		newDSH(),
		newCrush(),
		newChatBox(),
		newAider(),
		newQwenCode(),
		newContinueIDE(),
		newWorkBuddy(),
		newWorkBuddyAI(),
		newQoder(),
	}
}
