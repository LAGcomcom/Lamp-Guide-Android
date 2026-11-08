package core

import (
	"testing"
	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

func TestWorkBuddyRefreshOnlyUpdatesItsSourceAccount(t *testing.T) {
	a := chprov.Account{AccessToken: "old-a", Extra: map[string]string{"uid": "a"}}
	b := chprov.Account{ID: "b", AccessToken: "old-b"}
	cfg := &store.Config{WorkBuddy: lane.WBAccount{UID: "a", AccessToken: "old-a", Enabled: false}, ChannelAccounts: map[string]chprov.Accounts{"workbuddy": {a, b}}}
	fresh := lane.WBAccount{ChannelKey: a.Key(), UID: "a", AccessToken: "new-a", RefreshToken: "new-r", Enabled: true}
	persistWorkBuddyAccount(cfg, fresh)
	if cfg.ChannelAccounts["workbuddy"][0].AccessToken != "new-a" || cfg.ChannelAccounts["workbuddy"][1].AccessToken != "old-b" {
		t.Fatal("updated wrong account")
	}
	if cfg.WorkBuddy.AccessToken != "new-a" || cfg.WorkBuddy.Enabled {
		t.Fatal("refresh changed enable switch")
	}
	cfg.WorkBuddy = lane.WBAccount{}
	delete(cfg.ChannelAccounts, "workbuddy")
	persistWorkBuddyAccount(cfg, fresh)
	if cfg.WorkBuddy.AccessToken != "" || len(cfg.ChannelAccounts["workbuddy"]) != 0 {
		t.Fatal("refresh resurrected logged-out account")
	}
}

func TestWorkBuddyExplicitLoginAddsAndRefreshNeverAdds(t *testing.T) {
	cfg := &store.Config{}
	a := lane.WBAccount{UID: "a", AccessToken: "new-a", Enabled: true}
	persistWorkBuddyLogin(cfg, a)
	if cfg.WorkBuddy.AccessToken != "new-a" || len(cfg.ChannelAccounts["workbuddy"]) != 1 {
		t.Fatal("initial login not persisted")
	}
	persistWorkBuddyLogin(cfg, lane.WBAccount{UID: "b", AccessToken: "new-b", Enabled: true})
	if len(cfg.ChannelAccounts["workbuddy"]) != 2 || cfg.ChannelAccounts["workbuddy"][0].AccessToken != "new-a" {
		t.Fatal("new login replaced another account")
	}
	a.AccessToken = "rotated-a"
	persistWorkBuddyLogin(cfg, a)
	if len(cfg.ChannelAccounts["workbuddy"]) != 2 || cfg.ChannelAccounts["workbuddy"][0].AccessToken != "rotated-a" {
		t.Fatal("relogin duplicated identity")
	}
}
