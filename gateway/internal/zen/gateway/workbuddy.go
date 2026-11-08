package gateway

import (
	"context"
	"net/http"
	"sync"
	"time"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/logx"
	"zenui/internal/zen/store"
)

// The WorkBuddy 账号渠道's dashboard surface: a status readout for the
// settings card plus the three login-call endpoints. The heavy lifting —
// OAuth state polling, token rotation, persistence — lives in the lane; these
// handlers only compose what the card shows and trigger the catalog resync.

var (
	wbCreditsMu   sync.Mutex
	wbCreditsAt   time.Time
	wbCreditsLast string
)

// adminWorkBuddyStatus answers the settings card: login state, expiry,
// activity date, and a cached credits readout (the billing endpoint rides the
// upstream, so page refreshes share one answer per minute).
func (s *Server) adminWorkBuddyStatus(w http.ResponseWriter, r *http.Request) {
	// 读**折合后**的账号：渠道面登录写的是一直是 channelAccounts.workbuddy，
	// cfg.WorkBuddy 只在设置页那条登录路径上被写。只读后者会让「在渠道面登录
	// 成功、模型也在选择器里」的机器，在设置卡上显示成「未登录」—— 用户据此
	// 以为自己没登录过，再去登录一次。
	cfg := s.Store.Config()
	acct := lane.WBAccountFromChannel(cfg.ChannelAccounts["workbuddy"], cfg.WorkBuddy)
	// 登录态来自账号表，模型接入由持久化总开关控制，不能用转换后的 Enabled 代替。
	acct.Enabled = cfg.WorkBuddy.Enabled
	credits := ""
	detail := ""
	if acct.Enabled && acct.AccessToken != "" {
		wbCreditsMu.Lock()
		if time.Since(wbCreditsAt) < time.Minute {
			credits = wbCreditsLast
			wbCreditsMu.Unlock()
		} else {
			wbCreditsMu.Unlock()
			if v, err := s.Lane.WorkBuddyCredits(r.Context()); err == nil {
				credits = v
			} else {
				detail = err.Error()
			}
			wbCreditsMu.Lock()
			wbCreditsAt = time.Now()
			wbCreditsLast = credits
			wbCreditsMu.Unlock()
		}
	}
	writeJSON(w, 200, map[string]any{
		"loggedIn":         acct.AccessToken != "",
		"enabled":          acct.Enabled,
		"uid":              acct.UID,
		"nickname":         acct.Nickname,
		"expiresAtMs":      acct.ExpiresAtMS,
		"lastActivityDate": acct.LastActivityDate,
		"credits":          credits,
		"detail":           detail,
	})
}

// adminWorkBuddyLoginStart mints a login state and hands the auth URL back;
// the card opens it in a new tab and starts polling.
func (s *Server) adminWorkBuddyLoginStart(w http.ResponseWriter) {
	authURL, err := s.Lane.WorkBuddyLoginStart()
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Open the auth page in the user's DEFAULT browser, not a WebView2 popup:
	// the embedded window's profile is separate from their daily browser, so
	// a popup would ask for a login the browser session already has. Same
	// mechanism as the tray's 打开管理页.
	s.openURL(authURL)
	if s.logger != nil {
		s.logger.Infof("WorkBuddy 登录已开始，授权页已在默认浏览器打开")
	}
	writeJSON(w, 200, map[string]any{"ok": true, "authUrl": authURL})
}

// adminWorkBuddyLoginPoll advances the login once. "ok" means the pair is
// already persisted (through the lane's persist callback) and the roster is
// rebuilding; the handler only re-syncs the agent injections.
func (s *Server) adminWorkBuddyLoginPoll(w http.ResponseWriter) {
	state, err := s.Lane.WorkBuddyLoginPoll()
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "state": "error"})
		return
	}
	if state == "ok" {
		s.forgetQuota("workbuddy", "")
		acct := s.Store.Config().WorkBuddy
		s.logCat(logx.CatAdmin, "info", "WorkBuddy 账号已登录：%s（免费模型已进入目录）", wbAccountLabel(acct))
		if s.registry != nil {
			time.AfterFunc(1500*time.Millisecond, func() {
				if s.modelsSync != nil {
					s.modelsSync()
				}
				if n := s.registry.ResyncEnabled(); n > 0 && s.logger != nil {
					s.logger.Infof("WorkBuddy 渠道上线，已重新注入 %d 个已开启 Agent 的配置", n)
				}
			})
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "state": state})
}

// adminWorkBuddyLogout clears the account everywhere: store, lane snapshot,
// and the catalog (the next rebuild drops the roster).
func (s *Server) adminWorkBuddyLogout(w http.ResponseWriter) {
	/* 两张表都要清。凭据可能只落在 channelAccounts.workbuddy（渠道面那条登录
	   路径就是写它），而重新启动时 core 用 WBAccountFromChannel 折合账号 ——
	   渠道表那份优先。只清 cfg.WorkBuddy 的后果是「退出成功、重启又回来了」，
	   而界面上当时确实显示已退出。 */
	s.Store.Mutate(func(c *store.Config) {
		c.WorkBuddy = lane.WBAccount{}
		delete(c.ChannelAccounts, "workbuddy")
	})
	_ = s.Store.Save()
	s.Lane.WorkBuddyLogout()
	go s.Lane.RefreshCatalog(context.Background())
	if s.logger != nil {
		s.logger.Infof("WorkBuddy 账号已退出")
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func wbAccountLabel(a lane.WBAccount) string {
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.UID
}
