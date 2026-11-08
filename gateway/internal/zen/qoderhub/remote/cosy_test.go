package remote

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// MintCosyCredential 的形状是**上游契约**，不是口味问题：cosy_key 是 RSA-1024
// 包住的 16 字节 AES key（base64 后 172 字符），encrypt_user_info 是 AES-128-CBC
// 密文（base64 后随明文长度增长）。改成别的形状，登录会「成功」，而每一次模型
// 请求都 401 —— 这条断言就是拦那种改动的地方。
func TestMintCosyCredentialShape(t *testing.T) {
	key, info, err := MintCosyCredential("uid-1", "name", "a@b.c", "dt-token")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(key) != 172 {
		t.Fatalf("cosy_key 长度 %d，契约是 172（RSA-1024 包 16 字节 AES key）", len(key))
	}
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 128 {
		t.Fatalf("cosy_key 不是合法的 128 字节 RSA 密文：len=%d err=%v", len(raw), err)
	}

	sealed, err := base64.StdEncoding.DecodeString(info)
	if err != nil {
		t.Fatalf("encrypt_user_info 不是合法 base64：%v", err)
	}
	if len(sealed)%16 != 0 {
		t.Fatalf("encrypt_user_info 不是 AES 分组整数倍：%d", len(sealed))
	}

	// 同一身份两次铸出的 key 必须不同：AES key 是随机铸的，若相同说明它被写死，
	// 那样同一份凭据会在多台机器上互相覆盖。
	key2, _, err := MintCosyCredential("uid-1", "name", "a@b.c", "dt-token")
	if err != nil {
		t.Fatalf("mint twice: %v", err)
	}
	if key == key2 {
		t.Fatal("两次铸出的 cosy_key 相同：AES key 没有被随机生成")
	}
}

func TestMintCosyCredentialRejectsEmptyIdentity(t *testing.T) {
	if _, _, err := MintCosyCredential("", "n", "e", "t"); err == nil {
		t.Fatal("缺 uid 时必须失败：cosy 凭据里的 uid 与请求头里的 Cosy-User 必须同一人")
	}
	if _, _, err := MintCosyCredential("u", "n", "e", ""); err == nil {
		t.Fatal("缺 access token 时必须失败：没有它铸出来的凭据签发不了请求")
	}
}

// 网关自己的凭据优先；文件缺失或损坏时回落 IDE 缓存。
//
// ⚠️ 这条顺序是给用户的承诺（「渠道面登录过一次就不再依赖 IDE」），也是这条
// 中继能在全新机器上工作的前提：新机器上那个文件根本不存在，而 IDE 缓存可能
// 正是唯一可用的登录态。
func TestLoadCredentialFallsBackToIDECache(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "zen-gate-credential.json")

	// 文件不存在：不报错，落回 IDE 缓存（这台机器上没有缓存时才会报错，
	// 所以这里只断言「不是覆盖路径」这一件事）。
	cred, err := LoadCredential(override)
	if err != nil {
		if cred.Source == "" {
			t.Skip("本机既没有覆盖文件也没有 IDE 缓存，无法断言回落来源")
		}
		t.Fatalf("覆盖文件缺失时必须回落而不是报错：%v", err)
	}
	if cred.Source == override {
		t.Fatalf("覆盖文件不存在，来源却是它：%s", cred.Source)
	}

	// 合法覆盖文件：必须用它，且不读缓存。
	doc := map[string]any{
		"source": "qoder-device",
		"auth": map[string]string{
			"cosy_key": "k", "encrypt_user_info": "i",
			"user_id": "uid-x", "machine_id": "m", "access_token": "t",
		},
	}
	body, _ := json.Marshal(doc)
	if err := os.WriteFile(override, body, 0o600); err != nil {
		t.Fatalf("写入夹具: %v", err)
	}
	cred, err = LoadCredential(override)
	if err != nil {
		t.Fatalf("合法覆盖文件读失败：%v", err)
	}
	if cred.UserID != "uid-x" {
		t.Fatalf("没有用覆盖文件里的 uid：%q", cred.UserID)
	}
	if cred.Source != "qoder-device" {
		t.Fatalf("来源应是凭据里写的 qoder-device，实际 %q", cred.Source)
	}

	// 损坏的覆盖文件：回落，不让中继整段挂掉。
	if err := os.WriteFile(override, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("写入坏夹具: %v", err)
	}
	if cred, err = LoadCredential(override); err != nil {
		t.Fatalf("坏文件必须回落而不是报错：%v", err)
	}
	if cred.Source == override {
		t.Fatalf("坏文件被当成可用凭据：%s", cred.Source)
	}
}
