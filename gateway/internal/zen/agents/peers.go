package agents

import (
	"encoding/json"

	"zenui/internal/zen/store"
)

// Peers is what the host app knows about sibling apps on this device. It is
// the injection target that does NOT live in a file we can reach: on Android,
// DSH Go is a separate APK with its own proot rootfs behind app-private
// storage, so zen-gate can never write its cordis.patch.yml the way the
// desktop adapter does for ~/.dsh.
//
// Rather than pretend to inject and lie about the result, the dsh-go adapter
// reports what the host actually observed (is the APK installed, which
// version) and the page hands over the one thing that crosses that boundary:
// the gateway's own loopback endpoint plus its key, as a channel card the
// person pastes into DSH Go's 设置 › 模型 › 添加渠道.
//
// SetPeers is called once by the Android host before the gateway starts
// (mobile.SetPeers); a zero value simply means "no sibling app known", which
// every caller must render as unknown rather than as "not installed".
type PeerInfo struct {
	DSHGo        bool   `json:"dshGo"`
	DSHGoVersion string `json:"dshGoVersion,omitempty"`
	DSHGoPackage string `json:"dshGoPackage,omitempty"`
}

// Peers is the last known sibling-app state. Read-only after SetPeers.
var Peers PeerInfo

// peerKeyID is the AgentKeys entry a sibling-app channel uses. One per peer,
// so its traffic lands under its own name in the access log and the usage
// stats instead of blending into the main key's totals.
const peerKeyID = "dsh-go"

// mintPeerKey gives the sibling app a key of its own, once. A second call must
// not rotate it: the person may already have pasted the channel into the
// other app, and silently invalidating that is the worst possible outcome.
func mintPeerKey(st *store.Store) {
	if st == nil || st.Config().AgentKeys[peerKeyID] != "" {
		return
	}
	key := store.GenerateKey(peerKeyID)
	st.Mutate(func(cfg *store.Config) {
		if cfg.AgentKeys == nil {
			cfg.AgentKeys = map[string]string{}
		}
		cfg.AgentKeys[peerKeyID] = key
	})
	_ = st.Save()
}

// revokePeerKey closes the door the switch opened: a key that no longer shows
// on the page must not still open the gateway.
func revokePeerKey(st *store.Store) {
	if st == nil || st.Config().AgentKeys[peerKeyID] == "" {
		return
	}
	st.Mutate(func(cfg *store.Config) { delete(cfg.AgentKeys, peerKeyID) })
	_ = st.Save()
}

// SetPeersJSON records the host's report. An empty or unparseable payload
// clears it: "we were told nothing" is not "the app is missing".
func SetPeersJSON(raw string) {
	if raw == "" {
		Peers = PeerInfo{}
		return
	}
	var in PeerInfo
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		Peers = PeerInfo{}
		return
	}
	Peers = in
}
