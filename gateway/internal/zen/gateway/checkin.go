package gateway

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"zenui/internal/zen/channels"
	"zenui/internal/zen/checkin"
	"zenui/internal/zen/chprov"
	"zenui/internal/zen/store"
)

// StartCheckins ties automatic and explicit runs to the Core's lifetime.
func (s *Server) StartCheckins(ctx context.Context) {
	s.checkinStart.Do(func() {
		s.checkinCtx = ctx
		go s.checkins.Start(ctx)
		go s.nativeCatalogLoop(ctx)
	})
}

func (s *Server) checkinTargets() []checkin.Target {
	out := []checkin.Target{}
	for _, channel := range channels.Channels() {
		mode, reason := chprov.CheckinCapability(channel.ID)
		for _, account := range s.chanAccounts(channel.ID).Usable() {
			out = append(out, checkin.Target{Channel: channel.ID, Key: checkinKey(channel.ID, account), Label: channel.Name + " · " + account.Label(), Mode: mode, Reason: reason})
		}
	}
	return out
}

func checkinKey(channel string, account chprov.Account) string {
	if key := account.Field("lamp_checkin_key"); len(key) == 32 {
		if _, err := hex.DecodeString(key); err == nil {
			return key
		}
	}
	return checkin.AccountKey(channel, account.Key())
}

func (s *Server) claimAccount(ctx context.Context, target checkin.Target, day string) checkin.Outcome {
	if chprov.IsNativeChannel(target.Channel) {
		s.nativeAuthMu.Lock()
		defer s.nativeAuthMu.Unlock()
	}
	if s.checkinClient != nil {
		ctx = chprov.WithCheckinClient(ctx, s.checkinClient)
	}
	for _, account := range s.chanAccounts(target.Channel).Usable() {
		if checkinKey(target.Channel, account) != target.Key {
			continue
		}
		fresh, failure := chprov.CheckinAccount(ctx, target.Channel, account)
		if failure != nil {
			return *failure
		}
		if fresh.AccessToken != account.AccessToken || fresh.RefreshTok != account.RefreshTok {
			// OAuth device responses sometimes have no stable account ID. Preserve
			// the opaque check-in identity across a rotating-token fallback key.
			extra := make(map[string]string, len(fresh.Extra)+1)
			for key, value := range fresh.Extra {
				extra[key] = value
			}
			extra["lamp_checkin_key"] = target.Key
			fresh.Extra = extra
			// Never resurrect a logged-out account or overwrite a concurrent login.
			updated := false
			s.Store.Mutate(func(config *store.Config) {
				list := config.ChannelAccounts[target.Channel]
				for i, current := range list {
					if current.Key() == account.Key() && current.AccessToken == account.AccessToken && current.RefreshTok == account.RefreshTok {
						list[i] = fresh
						updated = true
						break
					}
				}
			})
			if !updated {
				return checkin.Outcome{Status: "failed", Message: "账号在签到期间发生变化，请重新补签"}
			}
		}
		if s.Store.Save() != nil {
			return checkin.Outcome{Status: "failed", Message: "登录凭据保存失败，未发送领取请求"}
		}
		result := chprov.ClaimDaily(ctx, target.Channel, fresh, day)
		if result.Status == "claimed" || result.Status == "already" {
			s.forgetQuota(target.Channel, account.Key())
		}
		return result
	}
	return checkin.Outcome{Status: "login_required", Message: "账号已退出，请重新登录"}
}

func (s *Server) adminCheckinState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.checkins.Snapshot())
}

func (s *Server) adminCheckinSettings(w http.ResponseWriter, r *http.Request) {
	var settings checkin.Settings
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&settings) != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSON(w, 400, map[string]any{"error": "签到设置格式错误"})
		return
	}
	known := map[string]bool{}
	for _, c := range channels.Channels() {
		known[c.ID] = true
	}
	for id := range settings.Disabled {
		if !known[id] {
			writeJSON(w, 400, map[string]any{"error": "签到设置包含未知渠道"})
			return
		}
	}
	if err := s.checkins.Update(settings); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, s.checkins.Snapshot())
}

func (s *Server) adminCheckinRun(w http.ResponseWriter, r *http.Request) {
	ctx := s.checkinCtx
	if ctx == nil {
		writeJSON(w, 503, map[string]any{"error": "签到调度尚未启动"})
		return
	}
	if err := s.checkins.Trigger(ctx); err != nil {
		writeJSON(w, 409, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 202, s.checkins.Snapshot())
}
