package agents

import (
	"testing"

	"zenui/internal/zen/store"
)

// The peer switch has to mean something: the key it mints is what server.agentOf
// resolves, so a sibling app's traffic stops hiding inside the main key's totals.
// Flipping the switch off must close that door.
func TestMintPeerKeyIsStableAndRevocable(t *testing.T) {
	t.Setenv("ZEN_GATE_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}

	mintPeerKey(st)
	key := st.Config().AgentKeys[peerKeyID]
	if key == "" {
		t.Fatal("mintPeerKey produced no key")
	}
	// A second mint must not rotate the key out from under a channel the
	// person may already have pasted into the other app.
	mintPeerKey(st)
	if got := st.Config().AgentKeys[peerKeyID]; got != key {
		t.Fatalf("second mint rotated the key: %q -> %q", key, got)
	}

	revokePeerKey(st)
	if got := st.Config().AgentKeys[peerKeyID]; got != "" {
		t.Fatalf("revokePeerKey left a usable key behind: %q", got)
	}
}
