package chprov

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 夹具：一条真实形状的凭据库（键名照参考插件的命名），其中既有本表认识的
// 7 家，也有**不该搬运**的键（OPENCODE_GO_API_KEY / ZEN_API_KEY）。
const fixture = `version: 1
refs:
  OPENCODE_GO_API_KEY: sk-opencode-x
  ZEN_API_KEY: ofm-zen-x
  ZCODE_ACCOUNT_AAA111:
    '{"zcode_jwt":"jwt-a","device_mid":"mid-a","zai_access_token":"zai-a","user_id":"uid-a","nickname":"Auto 10-01"}'
  CODEARTS_ACCOUNT_BBB222:
    '{"access_key_id":"AK","secret_access_key":"SK","security_token":"ST","user_name":"huawei"}'
  TRAE_ACCOUNT_CCC333:
    '{"access_token":"jwt-t","device_id":"dev","enterprise_id":"ent"}'
  QODER_ACCOUNT_DDD444:
    '{"security_oauth_token":"dt-x","access_token":"dt-x","refresh_token":"drt-x","expire_time":1795522408000,"machine_id":"m1","uid":"u1","nickname":"LAG"}'
  QODERCN_ACCOUNT_EEE555:
    '{"security_oauth_token":"dt-y","access_token":"dt-y","refresh_token":"drt-y","expire_time":1795522428000,"machine_id":"m2","uid":"u2"}'
  BUDDY_ACCOUNT_FFF666: '{"access_token":"jwt-b","refresh_token":"rt-b","user_id":"ub","nickname":"FFEE"}'
  WORKBUDDY_ACCOUNT_GGG777: '{"access_token":"jwt-w","refresh_token":"rt-w","user_id":"uw","nickname":"lagcomcom"}'
`

// 导入只搬白名单里的那几家：别的 key 挪进另一个渠道的槽位，症状是「到处都是 401」，
// 而 401 在面板上显示成「不可用」—— 用户会去查额度，而问题在凭据。
func TestImportOnlyTakesWhitelistedChannels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.yaml")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	t.Setenv("DSH_CREDENTIALS_FILE", path)

	accts, res := ImportAccounts()
	if res.Read != 7 {
		t.Fatalf("读到 %d 条，期望 7 条（夹具里有 7 个本表认识的键）", res.Read)
	}
	if len(accts) != 7 {
		t.Fatalf("导入 %d 家，期望 7 家；跳过: %v", len(accts), res.Skipped)
	}
	// 那两个不该搬运的键必须出现在 skipped 里，且理由写清「不属于本表」。
	for _, k := range []string{"OPENCODE_GO_API_KEY", "ZEN_API_KEY"} {
		why, ok := res.Skipped[k]
		if !ok {
			t.Fatalf("%s 没有被记录成跳过（那会让用户以为它也导入了）", k)
		}
		if !strings.Contains(why, "不属于") {
			t.Fatalf("%s 的跳过理由不够具体: %q", k, why)
		}
	}
	// 国际站不能吃中国站的键：白名单顺序错会把 minimaxintl 的 token 拿去查中国站。
	if list, ok := accts["minimaxintl"]; ok && len(list) > 0 {
		t.Fatal("夹具里没有 minimax 的键，却导入了一条")
	}
}

// 每家的附加字段必须逐家映射：zcode 缺 device_mid 时额度恒 401，codearts 缺
// AK/SK 时签名必然失败。而这些字段名各家不同，认错等于把凭据塞进错的槽位。
func TestImportMapsPerChannelFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.yaml")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	t.Setenv("DSH_CREDENTIALS_FILE", path)

	accts, _ := ImportAccounts()
	// 夹具里每家只有一条，取第一条断言字段映射。
	one := func(id string) Account {
		t.Helper()
		list := accts[id]
		if len(list) == 0 {
			t.Fatalf("没有导入 %s", id)
		}
		return list[0]
	}
	if got := one("zcode").Extra["device_mid"]; got != "mid-a" {
		t.Fatalf("zcode 的 device_mid = %q, want mid-a", got)
	}
	if got := one("codearts").Extra["access_key_id"]; got != "AK" {
		t.Fatalf("codearts 的 AK = %q", got)
	}
	if got := one("codearts").Extra["secret_access_key"]; got != "SK" {
		t.Fatalf("codearts 的 SK = %q", got)
	}
	if got := one("trae").Extra["device_id"]; got != "dev" {
		t.Fatalf("trae 的 device_id = %q", got)
	}
	if one("qoder").ExpiresAtMS != 1795522408000 {
		t.Fatalf("qoder 的 expire_time 没被读成毫秒: %d", one("qoder").ExpiresAtMS)
	}
	if got := one("qoder").Nickname; got != "LAG" {
		t.Fatalf("qoder 的昵称 = %q", got)
	}
	if got := one("buddy").Extra["uid"]; got != "ub" {
		t.Fatalf("buddy 的 uid = %q（额度端点与推理头都要它）", got)
	}
	// 每条导入都必须标明来源：排查「这个账号为什么不会自动续期」第一眼看的就是它。
	for id, list := range accts {
		for _, a := range list {
			if a.Source != "import" {
				t.Fatalf("%s 的来源 = %q, want import", id, a.Source)
			}
		}
	}
}

// 同一家渠道在凭据库里存了好几条时，**每一条都要进**：额度按账号算，
// 一个号烧完还有另一个号。此前只留一个槽位，「读到 22 条、导入 7 家」
// 而那 15 条被静默覆盖，用户以为它们本来就不存在。
func TestImportKeepsEveryAccountOfOneChannel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.yaml")
	body := "version: 1\nrefs:\n" +
		"  ZCODE_ACCOUNT_A: '{\"zcode_jwt\":\"jwt-a\",\"device_mid\":\"mid-a\",\"user_id\":\"uid-a\",\"nickname\":\"主号\"}'\n" +
		"  ZCODE_ACCOUNT_B: '{\"zcode_jwt\":\"jwt-b\",\"device_mid\":\"mid-b\",\"user_id\":\"uid-b\",\"nickname\":\"小号\"}'\n" +
		"  ZCODE_ACCOUNT_C: '{\"zcode_jwt\":\"jwt-a2\",\"device_mid\":\"mid-a\",\"user_id\":\"uid-a\",\"nickname\":\"主号\"}'\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	t.Setenv("DSH_CREDENTIALS_FILE", path)

	accts, res := ImportAccounts()
	list := accts["zcode"]
	if len(list) != 2 {
		t.Fatalf("导入 %d 个账号，期望 2 个（第三条是同一账号换了令牌，应被合并）", len(list))
	}
	// 同一账号的第三条要如实说「合并」，而不是静默吞掉。
	merged := 0
	for k, why := range res.Skipped {
		if strings.HasPrefix(k, "zcode/") && strings.Contains(why, "合并") {
			merged++
		}
	}
	if merged == 0 {
		t.Fatalf("同一账号的重复凭据没有被记录成合并: %v", res.Skipped)
	}
	// 两个号的 Key 必须不同：面板据此分开两行、也据此只退掉其中一个。
	if list[0].Key() == list[1].Key() {
		t.Fatalf("两个账号的 Key 相同（%q），面板会合成一行", list[0].Key())
	}
}

// 凭据库不存在时不许报错，更不许编造一条凭据：如实说「本机没有」。
func TestImportMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("DSH_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "nope.yaml"))
	accts, res := ImportAccounts()
	if len(accts) != 0 {
		t.Fatalf("文件不存在却导入了 %d 家", len(accts))
	}
	if !strings.Contains(res.Note, "没有参考插件") {
		t.Fatalf("说明不够具体: %q", res.Note)
	}
}

// 凭据认不出时必须跳过并说明，不能塞一条空 token 进去 ——
// 空 token 在面板上显示「已登录」，而每次请求都 401。
func TestUnparsableCredentialIsSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.yaml")
	body := "version: 1\nrefs:\n  ZCODE_ACCOUNT_X: 'not json at all'\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	t.Setenv("DSH_CREDENTIALS_FILE", path)
	accts, res := ImportAccounts()
	if list, ok := accts["zcode"]; ok && len(list) > 0 {
		t.Fatal("不可解析的凭据被导入了")
	}
	// 跳过理由按「渠道/键名」记：同一家的另一条好凭据不该被这一条的失败顶掉。
	why, ok := res.Skipped["zcode/ZCODE_ACCOUNT_X"]
	if !ok || !strings.Contains(why, "JSON") {
		t.Fatalf("跳过理由 = %q", why)
	}
}
