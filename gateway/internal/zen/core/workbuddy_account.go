package core

import (
	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// A rotated pair belongs to one stored identity. Removed identities stay removed.
func persistWorkBuddyAccount(c *store.Config, a lane.WBAccount) {
	legacy := chprov.Account{ID: c.WorkBuddy.UID, AccessToken: c.WorkBuddy.AccessToken}
	if c.WorkBuddy.AccessToken != "" && ((a.ChannelKey != "" && legacy.Key() == a.ChannelKey) || (a.UID != "" && c.WorkBuddy.UID == a.UID)) {
		enabled := c.WorkBuddy.Enabled
		c.WorkBuddy = a
		c.WorkBuddy.Enabled = enabled
	}
	list := c.ChannelAccounts["workbuddy"]
	for i := range list {
		match := list[i].Key() == a.ChannelKey
		if a.ChannelKey == "" {
			match = a.UID != "" && (list[i].ID == a.UID || list[i].Field("uid") == a.UID)
		}
		if !match {
			continue
		}
		list[i].AccessToken = a.AccessToken
		list[i].RefreshTok = a.RefreshToken
		if a.ExpiresAtMS > 0 {
			list[i].ExpiresAtMS = a.ExpiresAtMS
		}
	}
}

func persistWorkBuddyLogin(c *store.Config, a lane.WBAccount) {
	c.WorkBuddy = a
	if c.ChannelAccounts == nil {
		c.ChannelAccounts = map[string]chprov.Accounts{}
	}
	list := c.ChannelAccounts["workbuddy"]
	account := chprov.Account{ID: a.UID, Nickname: a.Nickname, Source: "poll"}
	for _, existing := range list {
		if existing.ID == a.UID || existing.Field("uid") == a.UID {
			account = existing
			break
		}
	}
	account.AccessToken = a.AccessToken
	account.RefreshTok = a.RefreshToken
	account.ExpiresAtMS = a.ExpiresAtMS
	if a.Nickname != "" {
		account.Nickname = a.Nickname
	}
	list.Upsert(account)
	c.ChannelAccounts["workbuddy"] = list
}
