package agents

import (
	"testing"

	"zenui/internal/zen/store"
)

// The host app's report is the only evidence the Agent page has about a
// sibling APK; "we were told nothing" must not read as "that app is missing".
func TestSetPeersJSONRoundTrip(t *testing.T) {
	t.Cleanup(func() { Peers = PeerInfo{} })

	SetPeersJSON(`{"dshGo":true,"dshGoVersion":"1.0.12","dshGoPackage":"io.github.ccch1mneyyy.dsh_app"}`)
	if !Peers.DSHGo {
		t.Fatalf("dshGo not recorded: %+v", Peers)
	}
	if Peers.DSHGoVersion != "1.0.12" || Peers.DSHGoPackage != "io.github.ccch1mneyyy.dsh_app" {
		t.Fatalf("fields = %+v", Peers)
	}

	// A malformed or empty payload clears the record instead of keeping the
	// previous answer: a stale "installed" is worse than an unknown one.
	SetPeersJSON("not json")
	if Peers.DSHGo {
		t.Fatalf("unparseable payload left a stale record: %+v", Peers)
	}

	SetPeersJSON(`{"dshGo":true}`)
	SetPeersJSON("")
	if Peers.DSHGo {
		t.Fatalf("empty payload left a stale record: %+v", Peers)
	}
}

// dsh-go is Android-only: its adapter targets another APK on the same phone.
// A desktop registry that carried it would show a permanently-unknown card.
func TestDesktopRegistryHasNoDSHGo(t *testing.T) {
	t.Setenv("ZEN_GATE_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(st)
	if _, ok := r.byID("dsh-go"); ok {
		t.Fatal("desktop registry should not carry the dsh-go adapter")
	}
}
