//go:build android

package agents

import "zenui/internal/zen/store"

// builtinAgents on Android is exactly the set whose target actually lives
// here. Editors and CLIs (ZCode, OpenCode, Codex, Claude, Crush, Aider,
// QwenCode, Continue) have no config file under the app's private storage,
// and 内置渠道登录 (WorkBuddy, Qoder) needs the desktop credential files
// those clients keep. Listing them would put a dozen permanently-off cards
// on the Agent page, each claiming "未安装" forever.
//
// So the Android list keeps what is real: dsh-go, the sibling app on this
// device. It cannot be injected by file (a separate APK, its own proot
// rootfs), so it hands over the gateway endpoint and key instead.
func builtinAgents(st *store.Store) []Agent {
	return []Agent{newDSHGo(st)}
}
