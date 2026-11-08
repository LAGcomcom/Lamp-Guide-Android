//go:build android

package agents

import "zenui/internal/zen/store"

// The dsh-go adapter exists only on Android. On the desktop there is a real
// adapter for DSH (dsh.go) because ~/.dsh/profiles/*/cordis.patch.yml is a
// path zen-gate owns. On Android DSH Go runs inside its own APK with its own
// proot rootfs, so that file is behind another app's private storage and no
// zen-gate build can write it.
//
// What remains is the half that does cross that boundary, and it is a real
// half: the gateway's loopback endpoint plus a key of its own. Enable therefore
// does two things that outlive the page —
//
//  1. mint the "dsh-go" AgentKey, so traffic from that app is attributed
//     separately in the access log and the usage stats (server.agentOf resolves
//     it; nothing else had ever minted one for a peer);
//  2. that is all — EnabledAgents["dsh-go"] is persisted by Registry.Enable.
//
// Disable revokes the key. Revoking is the honest behaviour: a key that no
// longer appears on the page but still opens the gateway is worse than making
// the person paste a fresh one.
type dshGo struct{ st *store.Store }

func newDSHGo(st *store.Store) Agent { return &dshGo{st: st} }

func (d *dshGo) Meta() (string, string, string) {
	return "dsh-go", "DSH Go",
		"用「设置 › 模型 › 添加渠道」接本网关（另一台 App 的文件系统，无法代写）"
}

func (d *dshGo) Detect() (bool, string, string) {
	if !Peers.DSHGo {
		return false, "", "未检测到 DSH Go"
	}
	detail := "已安装 · 通过自定义渠道接入"
	if Peers.DSHGoPackage != "" {
		detail = "已安装（" + Peers.DSHGoPackage + "）· 通过自定义渠道接入"
	}
	return true, Peers.DSHGoVersion, detail
}

// IsEnabled reads the person's decision, which is the only state this adapter
// owns. It is deliberately not derived from "is the app running": a channel
// pasted into DSH Go keeps working while the app is closed.
func (d *dshGo) IsEnabled() (bool, string, error) {
	if d.st == nil {
		return false, "", nil
	}
	return d.st.Config().EnabledAgents[peerKeyID], "", nil
}

// Enable mints the dedicated key. Minting is the switch: with a key of its own
// the other app's traffic is separable from everything else in 日志 and 用量.
func (d *dshGo) Enable(o Options) error {
	mintPeerKey(d.st)
	return nil
}

// Disable revokes the key so a switch flipped off actually closes the door.
func (d *dshGo) Disable() error {
	revokePeerKey(d.st)
	return nil
}
