package agents

import (
	"os"
	"path/filepath"
	"testing"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// A preview host runs on a machine whose ~/.config belongs to the real agents.
// Read-only keeps the page's report and stops the writes: without it, opening
// the preview's Agent page re-injected a throwaway gateway's key into the
// person's own ~/.dsh profiles (observed live, not hypothetical).
func TestReadOnlyRegistryWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZEN_GATE_HOME", home)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry(st)
	reg.SetEndpoints("http://127.0.0.1:1/v1", []lane.ModelInfo{{ID: "m1"}})
	reg.SetReadOnly(true)

	if n := reg.ResyncEnabled(); n != 0 {
		t.Fatalf("read-only ResyncEnabled wrote %d adapters", n)
	}
	// Enabling an adapter must record the switch but write no file.
	if err := reg.Enable("dsh-go"); err != nil && err.Error() != `unknown agent "dsh-go"` {
		t.Fatalf("Enable in read-only mode: %v", err)
	}
	if entries, _ := os.ReadDir(fakeHome); len(entries) > 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, filepath.Join(fakeHome, e.Name()))
		}
		t.Fatalf("read-only registry created %v", names)
	}
}

// The switch on Android has to actually do something: turning it on must mint
// the peer key (that is what makes the channel usable and attributable), and
// turning it off must revoke it. Read-only mode used to swallow this because
// DisableDesktop was folded into it.
func TestPeerKeySurvivesReadOnlyOff(t *testing.T) {
	t.Setenv("ZEN_GATE_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	mintPeerKey(st)
	if st.Config().AgentKeys[peerKeyID] == "" {
		t.Fatal("mintPeerKey produced no key with read-only off")
	}
	revokePeerKey(st)
	if st.Config().AgentKeys[peerKeyID] != "" {
		t.Fatal("revokePeerKey left the key behind")
	}
}
