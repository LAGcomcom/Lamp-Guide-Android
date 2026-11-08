package chprov

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 本文件是「授权从哪来」的第五条路径：**导入本机已存在的授权**。
//
// # 为什么这条路径必须有
//
// 用户在这台机器上已经装过参考插件，并在那边**真的登录过** 7 家渠道
// （zcode / codearts / buddy / workbuddy / qoder / qodercn / trae）。
// 那些凭据存在 ~/.dsh/.credentials.yaml 里，键名形如 ZCODE_ACCOUNT_XXXXXXXX。
//
// 不接这条路径，用户就得在 7 家里逐个重新扫一次码 —— 而那些凭据**就在盘上**，
// 且是用户自己在那一边授权的。参考实现的做法也是同一件事：各家 *-auth 都先找
// 「本机既有会话」，找不到才走新授权。
//
// # 三条纪律
//
//  1. **只读，绝不回写**：那些文件是插件与客户端的资产。写坏的后果由用户的
//     登录态承担，而我们只是「借用一次」。
//  2. **绝不搬运我们不认识的键**：只读 refs 段里白名单前缀那几家，其余键
//     （OPENCODE_GO_API_KEY / ZEN_API_KEY 之类）一律不碰 —— 把一份 key 挪进
//     另一个渠道的槽位，症状是「到处都是 401」。
//  3. **导入要如实报「导入了哪几家、跳过了哪几家、为什么」**：一次静默的部分
//     成功比失败更难排查 —— 用户会以为「已经导入了」，而实际只进来 3 家。

// PluginCredentialPath 返回参考插件凭据库的默认位置。
//
// ⚠️ DSH_CREDENTIALS_FILE 覆盖时的语义是「换一个文件读」，不是「关掉导入」——
// 测试要能指向一个夹具文件，否则这条路径只能靠「这台机器装过插件」来验证。
func PluginCredentialPath() string {
	if v := strings.TrimSpace(os.Getenv("DSH_CREDENTIALS_FILE")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".dsh", ".credentials.yaml")
}

// importPrefixes 是**允许搬运**的键前缀与渠道 id 的对应表。
//
// ⚠️ 这是白名单，不是「凡是 ACCOUNT_ 结尾的都搬」：参考插件录入的账号键还有
// astudio / raccoon / minimax / magicrew 等渠道，而它们的凭据形状（cookie 四字段、
// AK/SK 三件套）与本表各家的字段名并不通用。
var importPrefixes = []struct {
	prefix string
	id     string
}{
	{"ASTUDIO_ACCOUNT_", "astudio"},
	{"AUTOCLAWINTL_ACCOUNT_", "autoclawintl"},
	{"AUTOCLAW_ACCOUNT_", "autoclaw"},
	{"MAGICREWINTL_ACCOUNT_", "magicrewintl"},
	{"MAGICREW_ACCOUNT_", "magicrew"},
	{"WORKBUDDY_ACCOUNT_", "workbuddy"},
	{"BUDDY_ACCOUNT_", "buddy"},
	{"QODERCN_ACCOUNT_", "qodercn"},
	{"QODER_ACCOUNT_", "qoder"},
	{"CODEARTS_ACCOUNT_", "codearts"},
	{"TRAE_ACCOUNT_", "trae"},
	{"ZCODE_ACCOUNT_", "zcode"},
	{"CLINE_ACCOUNT_", "cline"},
	{"LOBSTERAI_ACCOUNT_", "lobsterai"},
	{"LOOMY_ACCOUNT_", "loomy"},
	{"MINIMAX_INTL_ACCOUNT_", "minimaxintl"},
	{"MINIMAX_ACCOUNT_", "minimax"},
	{"ACCIO_ACCOUNT_", "accio"},
	{"OFFICEACE_ACCOUNT_", "officeace"},
	{"RACCOON_ACCOUNT_", "raccoon"},
}

// ImportResult 是一次导入的结论。
//
// ⚠️ 三栏都要给：只报 imported 的话，一次「读到 7 条、导入 3 条」的部分成功
// 在面板上就等于「成功」—— 用户会以为已经全好了。
type ImportResult struct {
	Path     string            `json:"path"`
	Read     int               `json:"read"`
	Imported []ImportedChannel `json:"imported"`
	Skipped  map[string]string `json:"skipped"`
	Note     string            `json:"note,omitempty"`
}

// ImportedChannel 是导入成功的一条：只有渠道 id、账号标签与凭据来源，
// **不回显令牌**。
//
// Key 是这一条在渠道内的稳定身份（与 Account.Key() 同源），面板据此说
// 「这一家新增了哪几个号」，而不必再自己算一遍。
type ImportedChannel struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	Key      string `json:"key,omitempty"`
	Source   string `json:"source"`
}

// ImportAccounts 读参考插件的凭据库，把本表认识的渠道逐条折成 Accounts。
//
// 返回值是**派生事实**，不写盘：调用方拿到之后自己决定要不要落进
// store.ChannelAccounts。这样「导入」这件事在单测里可断言，而不必先在这台
// 机器上装一个插件。
//
// # 一家多条，不是一家一条
//
// 参考插件的凭据库对同一家渠道会存好几条（ZCODE_ACCOUNT_A…、ZCODE_ACCOUNT_B…），
// 每条是一个真账号：额度按账号算，一个号烧完还有另一个号。此前这里只留一个槽位，
// 症状是「读到 22 条、导入 7 家」，其余 15 条被静默覆盖 —— 用户以为那些号本来
// 就不存在。现在按 Account.Key() 去重后全部带上。
func ImportAccounts() (map[string]Accounts, ImportResult) {
	out := map[string]Accounts{}
	res := ImportResult{Skipped: map[string]string{}}
	path := PluginCredentialPath()
	res.Path = path
	if path == "" {
		res.Note = "找不到用户主目录"
		return out, res
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		res.Note = "本机没有参考插件的凭据库"
		return out, res
	}
	var doc struct {
		Refs map[string]string `json:"refs"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		res.Note = "凭据库不是可解析的 YAML：" + err.Error()
		return out, res
	}
	if doc.Refs == nil {
		res.Note = "凭据库里没有 refs 段"
		return out, res
	}
	keys := make([]string, 0, len(doc.Refs))
	for k := range doc.Refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(doc.Refs[k])
		if v == "" {
			continue
		}
		id := ""
		for _, e := range importPrefixes {
			if strings.HasPrefix(k, e.prefix) {
				id = e.id
				break
			}
		}
		if id == "" {
			res.Skipped[k] = "不属于本表里的任何渠道（不搬运）"
			continue
		}
		res.Read++
		acct, why := accountFromPluginJSON(id, v)
		if why != "" {
			res.Skipped[id+"/"+k] = why
			continue
		}
		// 同一渠道的多条都收下：每一条是一个独立账号（额度按账号算）。
		// 去重按 Account.Key()—— 同一个账号在库里出现两遍（换过一次令牌）时
		// 不该在面板上排成两行，否则用户会以为有两个号、并且退掉一个之后
		// 另一个还在，而它们其实是同一份凭据。
		list := out[id]
		before := len(list)
		list.Upsert(acct)
		out[id] = list
		if len(list) == before {
			res.Skipped[id+"/"+k] = "同一账号的凭据已经读过一条，这一条被合并"
			continue
		}
		res.Imported = append(res.Imported, ImportedChannel{
			ID: id, Nickname: acct.Label(), Key: acct.Key(), Source: acct.Source,
		})
	}
	return out, res
}

// accountFromPluginJSON 把参考插件的一条凭据 JSON 折成本表的 Account。
//
// 每家的字段名都不一样（这是事实，不是可以统一的形状），故逐家点名：
//
//	zcode      zcode_jwt + device_mid + zai_access_token + user_id
//	codearts   access_key_id + secret_access_key + security_token
//	trae       access_token + uid + device_id
//	qoder      access_token + refresh_token + expire_time + uid
//	buddy      access_token + refresh_token + uid + nickname
//
// ⚠️ 认不出就**跳过并说明**，不硬塞：把 zcode 的 jwt 塞进 trae 的槽位，
// 症状是那一家恒 401，而 401 在面板上显示成「不可用」—— 看起来像没额度。
func accountFromPluginJSON(id, raw string) (Account, string) {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		// WorkBuddy / Buddy 的凭据是单引号包着的 JSON，YAML 读出来的字符串
		// 里含换行（scope 字段就断行了）。先压成一行再解一次。
		if json.Unmarshal([]byte(collapseWS(raw)), &m) != nil {
			return Account{}, "凭据不是可解析的 JSON"
		}
	}
	acct := Account{Extra: map[string]string{}, Source: "import", CreatedAtMS: time.Now().UnixMilli()}
	acct.AccessToken = firstNonEmpty(field(m, "access_token"), field(m, "zcode_jwt"), field(m, "security_oauth_token"))
	acct.RefreshTok = firstNonEmpty(field(m, "refresh_token"), field(m, "refreshToken"))
	acct.Nickname = firstNonEmpty(field(m, "nickname"), field(m, "account_label"), field(m, "account_name"))
	acct.ID = firstNonEmpty(field(m, "user_id"), field(m, "uid"))
	if acct.ID != "" {
		acct.Extra["uid"] = acct.ID
	}
	// 到期时刻：几家写毫秒，qoder 写 expire_time（毫秒）。
	if v, ok := numeric(m["expire_time"]); ok && v > 0 {
		acct.ExpiresAtMS = int64(v)
	}
	if v, ok := numeric(m["expires_at"]); ok && v > 0 {
		acct.ExpiresAtMS = int64(v)
	}
	// 逐家的附加字段（额度端点与推理头都要用）。
	switch id {
	case "astudio":
		acct.AccessToken = firstNonEmpty(acct.AccessToken, field(m, "modelBearerToken"))
		acct.ID = firstNonEmpty(acct.ID, field(m, "account_id"), field(m, "accountId"))
		acct.Extra["account_id"] = firstNonEmpty(field(m, "account_id"), field(m, "accountId"))
		acct.Extra["sso_session_id"] = firstNonEmpty(field(m, "sso_session_id"), field(m, "ssoSessionId"))
		acct.Extra["token"] = field(m, "token")
	case "autoclaw", "autoclawintl":
		acct.AccessToken = cleanRewardToken(acct.AccessToken)
		if acct.ExpiresAtMS > 0 && acct.ExpiresAtMS < 1e11 {
			acct.ExpiresAtMS *= 1000
		}
		if sub, _, exp, ok := JWTClaims(acct.AccessToken); ok {
			if acct.ID == "" {
				acct.ID = sub
			}
			if exp > 0 {
				acct.ExpiresAtMS = exp
			}
		}
		acct.Extra["device_id"] = field(m, "device_id")
	case "magicrew", "magicrewintl":
		acct.AccessToken = field(m, "jwt") // management JWT; never substitute the inference API key
		acct.Extra["organization_code"] = field(m, "organizationCode")
		acct.Extra["api_key"] = field(m, "apiKey")
		acct.Extra["api_key_expires_at"] = field(m, "apiKeyExpiresAt")
		if sub, _, exp, ok := JWTClaims(acct.AccessToken); ok {
			acct.ID, acct.ExpiresAtMS = sub, exp
		}
	case "zcode":
		acct.Extra["device_mid"] = field(m, "device_mid")
		acct.Extra["zai_access_token"] = field(m, "zai_access_token")
	case "codearts":
		acct.Extra["access_key_id"] = field(m, "access_key_id")
		acct.Extra["secret_access_key"] = field(m, "secret_access_key")
		acct.Extra["security_token"] = field(m, "security_token")
	case "trae":
		acct.Extra["device_id"] = field(m, "device_id")
	case "qoder", "qodercn":
		acct.Extra["machine_id"] = field(m, "machine_id")
	case "loomy":
		acct.Extra["session"] = field(m, "session")
		acct.Extra["access_key"] = field(m, "access_key")
		acct.Extra["secret_key"] = field(m, "secret_key")
	case "officeace":
		acct.Extra["cookie"] = field(m, "cookie")
	case "raccoon":
		acct.Extra["device_id"] = field(m, "device_id")
		acct.Extra["org"] = field(m, "office_identity")
	case "gemini":
		acct.Extra["project"] = firstNonEmpty(field(m, "cloudaicompanionProject"), field(m, "project"))
	}
	// 空的附加字段不留在 Extra 里：一个空串会让「缺 device_mid」的判据失效。
	for k, v := range acct.Extra {
		if strings.TrimSpace(v) == "" {
			delete(acct.Extra, k)
		}
	}
	if acct.AccessToken == "" && acct.Extra["cookie"] == "" && acct.Extra["access_key_id"] == "" {
		return Account{}, "凭据里没有可用的 token（字段名认不出）"
	}
	// 缺了这家额度端点要的附加字段时，**如实报**而不是导入一条发不出去的凭据：
	// 导入之后面板显示「已登录」而额度恒 401，比「没导入」更难排查。
	//
	// ⚠️ 只校验「额度端点真会用到的」那几个（Spec.QuotaExtraKeys）。把
	// Spec.ExtraKeys 整个拿来校验是不对的：那里有 uid / project / account_id
	// 这类**由令牌或后端派生**的字段，缺它们不影响读额度。
	for _, k := range QuotaExtraKeys(id) {
		if acct.Extra[k] == "" {
			return Account{}, "凭据缺少额度端点要的字段 " + k
		}
	}
	return acct, ""
}

// field 取一个字符串字段（数字也按字符串给出：上游的 uid 时而数字时而字符串）。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// numericField 读一个可能是数字、也可能是字符串的字段。
func numericField(m map[string]any, key string) (float64, bool) {
	return toNum(m[key])
}

// collapseWS 把多行字符串压成一行（YAML 的折叠与字面块会把 JSON 拆行）。
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
