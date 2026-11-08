package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

func (s *Server) syncOpenCode() {
	s.Lane.SetOpenCodeAccounts(s.Store.Config().ChannelAccounts["opencode"])
}

func (s *Server) persistOpenCode(a chprov.Account, states map[string]lane.OpenCodeState) {
	encoded, _ := json.Marshal(states)
	s.Store.Mutate(func(c *store.Config) {
		if c.ChannelAccounts == nil {
			c.ChannelAccounts = map[string]chprov.Accounts{}
		}
		list := c.ChannelAccounts["opencode"]
		for i, current := range list {
			if current.Key() != a.Key() {
				continue
			}
			if lane.OpenCodeIdentity(current) != lane.OpenCodeIdentity(a) {
				return
			}
			if current.Extra == nil {
				current.Extra = map[string]string{}
			}
			current.Extra["oc_states"] = string(encoded)
			list[i] = current
			c.ChannelAccounts["opencode"] = list
			return
		}
		if a.ID == lane.OpenCodeAnonymousID {
			a.Extra = map[string]string{"oc_states": string(encoded)}
			c.ChannelAccounts["opencode"] = append(list, a)
		}
	})
	if err := s.Store.Save(); err != nil && s.logger != nil {
		s.logger.Errorf("OpenCode 状态保存失败: %v", err)
	}
}

func (s *Server) adminOpenCode(w http.ResponseWriter, r *http.Request, action string) {
	if action == "state" && r.Method == http.MethodGet {
		cat, availability, _ := s.Lane.Snapshot()
		models := []map[string]any{}
		seen := map[string]bool{}
		for _, m := range cat {
			base := lane.BaseModelId(m.ID)
			if m.Channel != "" || m.SystemOne || m.Wire == "systemone" || seen[base] {
				continue
			}
			seen[base] = true
			models = append(models, map[string]any{"id": base, "name": m.Name, "state": availability[m.ID].State})
		}
		writeJSON(w, 200, map[string]any{"accounts": s.Lane.OpenCodeViews(), "models": models, "now": time.Now().UnixMilli()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	var in struct {
		Key       string  `json:"key"`
		Token     string  `json:"token"`
		Nickname  string  `json:"nickname"`
		Anonymous bool    `json:"anonymous"`
		Enabled   *bool   `json:"enabled"`
		ProxyMode string  `json:"proxyMode"`
		ProxyURL  *string `json:"proxyUrl"`
		Model     string  `json:"model"`
	}
	if err := decodeBody(r, &in); err != nil {
		writeJSON(w, 400, map[string]any{"error": "请求格式无效"})
		return
	}
	fail := func(code int, msg string) { writeJSON(w, code, map[string]any{"error": msg}) }
	if action == "probe" {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		result, err := s.Lane.ProbeOpenCode(ctx, in.Key, in.Model)
		if err != nil {
			fail(400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "result": result})
		return
	}
	if action == "proxy-test" {
		var account chprov.Account
		if in.Key == "id:"+lane.OpenCodeAnonymousID {
			account = lane.DefaultOpenCodeAccount()
		}
		for _, a := range s.Store.Config().ChannelAccounts["opencode"] {
			if a.Key() == in.Key {
				account = a
				break
			}
		}
		if account.AccessToken == "" {
			fail(404, "账号不存在")
			return
		}
		if in.ProxyMode != "" {
			account.Extra = copyOpenCodeExtra(account.Extra)
			account.Extra["oc_proxy_mode"] = in.ProxyMode
		}
		if in.ProxyURL != nil {
			account.Extra = copyOpenCodeExtra(account.Extra)
			account.Extra["oc_proxy_url"] = strings.TrimSpace(*in.ProxyURL)
		}
		client, err := lane.OpenCodeClient(account)
		if err != nil {
			fail(400, err.Error())
			return
		}
		if client != lane.Client() {
			defer client.CloseIdleConnections()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		start := time.Now()
		eg := lane.DetectEgressVia(ctx, client)
		if eg.IP == "" {
			fail(502, "未能获取出口 IP，请检查代理地址、端口及网络连接")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "ip": eg.IP, "country": eg.Country, "latencyMs": time.Since(start).Milliseconds()})
		return
	}
	if action != "add" && action != "settings" && action != "reset" && action != "delete" {
		fail(404, "not found")
		return
	}
	s.ocAdminMu.Lock()
	defer s.ocAdminMu.Unlock()
	var opErr error
	s.Store.Mutate(func(c *store.Config) {
		if c.ChannelAccounts == nil {
			c.ChannelAccounts = map[string]chprov.Accounts{}
		}
		list := c.ChannelAccounts["opencode"]
		if action == "add" {
			token := strings.TrimSpace(in.Token)
			if in.Anonymous {
				token = "public"
			}
			if token != "public" && (len(token) < 20 || len(token) > 4096 || strings.ContainsAny(token, " \t\r\n")) {
				opErr = fmt.Errorf("请填写完整的 OpenCode API Key")
				return
			}
			if !in.Anonymous && token == "public" {
				opErr = fmt.Errorf("匿名通道请使用「添加匿名通道」")
				return
			}
			if !in.Anonymous {
				for _, a := range list {
					if a.AccessToken == token {
						opErr = fmt.Errorf("这个 API Key 已存在")
						return
					}
				}
			}
			sum := sha256.Sum256([]byte(token))
			id := "oc-" + hex.EncodeToString(sum[:8])
			if in.Anonymous {
				id = "oc-anon-" + store.GenerateKey("")[:12]
			}
			nick := strings.TrimSpace(in.Nickname)
			if len([]rune(nick)) > 80 {
				opErr = fmt.Errorf("名称最多 80 个字符")
				return
			}
			if nick == "" {
				if in.Anonymous {
					nick = "匿名通道"
				} else {
					nick = "OpenCode 账号"
				}
			}
			c.ChannelAccounts["opencode"] = append(list, chprov.Account{ID: id, Nickname: nick, AccessToken: token, CreatedAtMS: time.Now().UnixMilli(), Source: "paste"})
			return
		}
		index := -1
		for i, a := range list {
			if a.Key() == in.Key {
				index = i
				break
			}
		}
		if index < 0 && in.Key == "id:"+lane.OpenCodeAnonymousID {
			list = append(list, lane.DefaultOpenCodeAccount())
			index = len(list) - 1
		}
		if index < 0 {
			opErr = fmt.Errorf("账号不存在")
			return
		}
		a := list[index]
		a.Extra = copyOpenCodeExtra(a.Extra)
		if action == "delete" {
			if a.ID == lane.OpenCodeAnonymousID {
				opErr = fmt.Errorf("默认匿名通道请使用停用")
				return
			}
			c.ChannelAccounts["opencode"] = append(list[:index:index], list[index+1:]...)
			return
		}
		if action == "settings" {
			if in.Enabled != nil {
				a.Extra["oc_disabled"] = fmt.Sprint(!*in.Enabled)
			}
			if in.ProxyMode != "" {
				if in.ProxyMode != "inherit" && in.ProxyMode != "direct" && in.ProxyMode != "custom" {
					opErr = fmt.Errorf("未知代理模式")
					return
				}
				a.Extra["oc_proxy_mode"] = in.ProxyMode
			}
			if in.ProxyURL != nil {
				a.Extra["oc_proxy_url"] = strings.TrimSpace(*in.ProxyURL)
			}
			if a.Extra["oc_proxy_mode"] == "custom" {
				normalized, err := lane.NormalizeOpenCodeProxy(a.Extra["oc_proxy_url"])
				if err != nil {
					opErr = err
					return
				}
				a.Extra["oc_proxy_url"] = normalized
			} else {
				delete(a.Extra, "oc_proxy_url")
			}
		}
		delete(a.Extra, "oc_states")
		a.Extra["oc_revision"] = fmt.Sprint(time.Now().UnixNano())
		list[index] = a
		c.ChannelAccounts["opencode"] = list
	})
	if opErr != nil {
		fail(400, opErr.Error())
		return
	}
	if err := s.Store.Save(); err != nil {
		s.syncOpenCode()
		fail(500, "更改已在内存中生效，但保存失败："+err.Error())
		return
	}
	s.syncOpenCode()
	s.forgetQuota("opencode", "")
	writeJSON(w, 200, map[string]any{"ok": true})
}

func copyOpenCodeExtra(src map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range src {
		out[k] = v
	}
	return out
}
