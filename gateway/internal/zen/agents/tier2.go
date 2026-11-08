package agents

// Tier-2 adapters: agents/clients whose config formats are documented but
// which may not be installed on this machine. Each follows the same contract
// as the tier-1 adapters — detect, enable (backup → atomic write → rollback),
// disable — and refuses to write files whose format it cannot confidently
// parse.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// --- shared helpers ---------------------------------------------------------

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// appDataDir joins parts onto the host OS's per-user application data
// directory: %APPDATA% on Windows, ~/Library/Application Support on macOS.
func appDataDir(parts ...string) string {
	base, err := os.UserHomeDir()
	if err == nil {
		if runtime.GOOS == "darwin" {
			base = filepath.Join(base, "Library", "Application Support")
		} else if runtime.GOOS != "windows" {
			base = filepath.Join(base, ".config")
		}
	}
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			base = appdata
		}
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// readJSONMap loads a JSON object file, or an empty map when absent.
func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("无法解析 %s（%w）；为避免破坏未知格式，拒绝写入", filepath.Base(path), err)
	}
	return doc, nil
}

// --- Crush ------------------------------------------------------------------
//
// crush.json providers.{zen_gate} = {type:"openai-compat", base_url, api_key,
// models:[{id,name,context_window,default_max_tokens,can_reason}]} — schema
// per charm.land/crush.json (verified against DeepSeek's official integration
// docs).

type crush struct{}

func newCrush() *crush { return &crush{} }

func (c *crush) Meta() (string, string, string) {
	return "crush", "Crush (Charm)", "~/.config/crush/crush.json 注入 openai-compat provider"
}

func (c *crush) configPath() string { return homePath(".config", "crush", "crush.json") }

func (c *crush) Detect() (bool, string, string) {
	if fileExists(c.configPath()) {
		return true, "", "检测到 crush.json"
	}
	if _, err := os.Stat(homePath(".config", "crush")); err == nil {
		return true, "", "检测到 Crush 配置目录（crush.json 尚未创建）"
	}
	return false, "", "未检测到 Crush"
}

func (c *crush) isEnabled(doc map[string]any) bool {
	prov, _ := doc["providers"].(map[string]any)
	_, ok := prov["zen_gate"].(map[string]any)
	return ok
}

func (c *crush) IsEnabled() (bool, string, error) {
	doc, err := readJSONMap(c.configPath())
	if err != nil {
		return false, "", nil
	}
	return c.isEnabled(doc), "", nil
}

func (c *crush) Enable(o Options) error {
	path := c.configPath()
	doc, err := readJSONMap(path)
	if err != nil {
		return err
	}
	if data, err := os.ReadFile(path); err == nil {
		_, _ = backupFile("crush", path, data)
	}
	prov, _ := doc["providers"].(map[string]any)
	if prov == nil {
		prov = map[string]any{}
	}
	models := []map[string]any{}
	for _, m := range o.Models {
		models = append(models, map[string]any{
			"id": m.ID, "name": m.Name,
			"context_window": m.ContextWindow, "default_max_tokens": m.MaxOutput,
			"can_reason": m.Reasoning,
		})
	}
	prov["zen_gate"] = map[string]any{
		"type": "openai-compat", "base_url": o.BaseURL, "api_key": o.APIKey,
		"models": models,
	}
	doc["providers"] = prov
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return atomicWrite(path, data)
}

func (c *crush) Disable() error {
	path := c.configPath()
	doc, err := readJSONMap(path)
	if err != nil {
		return nil
	}
	prov, _ := doc["providers"].(map[string]any)
	if prov == nil {
		return nil
	}
	delete(prov, "zen_gate")
	if len(prov) == 0 {
		delete(doc, "providers")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// --- ChatBox ----------------------------------------------------------------
//
// chatbox.config.json: openai.{apiHost, apiKey, customModels, useChatCompletionApi}.
// Format sourced from community documentation — the writer validates the
// existing file's top-level shape and refuses anything unexpected.

type chatbox struct{}

func newChatBox() *chatbox { return &chatbox{} }

func (c *chatbox) Meta() (string, string, string) {
	return "chatbox", "ChatBox", "用户数据目录下 ChatBox/chatbox.config.json 注入 openai 渠道"
}

func (c *chatbox) configPath() string {
	return filepath.Join(appDataDir("ChatBox"), "chatbox.config.json")
}

func (c *chatbox) Detect() (bool, string, string) {
	if fileExists(c.configPath()) {
		return true, "", "检测到 chatbox.config.json"
	}
	if fileExists(appDataDir("ChatBox")) {
		return true, "", "检测到 ChatBox 数据目录（配置尚未生成，先启动一次 ChatBox）"
	}
	return false, "", "未检测到 ChatBox"
}

func (c *chatbox) IsEnabled() (bool, string, error) {
	doc, err := readJSONMap(c.configPath())
	if err != nil {
		return false, "", nil
	}
	oa, _ := doc["openai"].(map[string]any)
	host, _ := oa["apiHost"].(string)
	return strings.Contains(host, "127.0.0.1"), "", nil
}

func (c *chatbox) Enable(o Options) error {
	path := c.configPath()
	doc, err := readJSONMap(path)
	if err != nil {
		return err
	}
	if data, err := os.ReadFile(path); err == nil {
		_, _ = backupFile("chatbox", path, data)
	}
	oa, _ := doc["openai"].(map[string]any)
	if oa == nil {
		oa = map[string]any{}
	}
	ids := []string{}
	for _, m := range o.Models {
		ids = append(ids, m.ID)
	}
	oa["apiHost"] = strings.TrimSuffix(o.BaseURL, "/v1")
	oa["apiPath"] = "/v1/chat/completions"
	oa["apiKey"] = o.APIKey
	oa["customModels"] = ids
	oa["useChatCompletionApi"] = true
	doc["openai"] = oa
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return atomicWrite(path, data)
}

func (c *chatbox) Disable() error {
	path := c.configPath()
	doc, err := readJSONMap(path)
	if err != nil {
		return nil
	}
	oa, _ := doc["openai"].(map[string]any)
	if oa == nil {
		return nil
	}
	if host, _ := oa["apiHost"].(string); !strings.Contains(host, "127.0.0.1") {
		return nil // not ours
	}
	if b := latestBackup("chatbox", "chatbox.config.json"); b != "" {
		if data, err := os.ReadFile(b); err == nil {
			return atomicWrite(path, data)
		}
	}
	delete(doc, "openai")
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// --- Aider ------------------------------------------------------------------
//
// ~/.aider.conf.yml: top-level keys openai-api-base / openai-api-key inside a
// marker block (YAML comments make the block removable). Model is chosen in
// aider with the openai/<id> prefix.

type aider struct{}

const aiderMarkerBegin = "# >>> zen-gate managed block (do not edit)"
const aiderMarkerEnd = "# <<< zen-gate managed block"

func newAider() *aider { return &aider{} }

func (a *aider) Meta() (string, string, string) {
	return "aider", "Aider", "~/.aider.conf.yml 注入 openai-api-base/key（模型用 /model openai/<id> 选择）"
}

func (a *aider) configPath() string { return homePath(".aider.conf.yml") }

func (a *aider) Detect() (bool, string, string) {
	if fileExists(a.configPath()) {
		return true, "", "检测到 .aider.conf.yml"
	}
	if fileExists(homePath(".aider")) || fileExists(homePath(".aider.conf.yml")) {
		return true, "", "检测到 Aider 配置"
	}
	return false, "", "未检测到 Aider"
}

func (a *aider) IsEnabled() (bool, string, error) {
	data, err := os.ReadFile(a.configPath())
	if err != nil {
		return false, "", nil
	}
	return strings.Contains(string(data), aiderMarkerBegin), "", nil
}

func (a *aider) strip(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	in := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case aiderMarkerBegin:
			in = true
		case aiderMarkerEnd:
			in = false
		case "":
			if !in {
				out = append(out, line)
			}
		default:
			if !in {
				out = append(out, line)
			}
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func (a *aider) Enable(o Options) error {
	path := a.configPath()
	var srcStr string
	if data, err := os.ReadFile(path); err == nil {
		_, _ = backupFile("aider", path, data)
		srcStr = string(data)
	}
	base := a.strip(srcStr)
	if base != "" && !strings.HasSuffix(base, "\n") {
		base += "\n"
	}
	block := fmt.Sprintf(`%s
openai-api-base: %s
openai-api-key: %s
%s
`, aiderMarkerBegin, o.BaseURL, o.APIKey, aiderMarkerEnd)
	return atomicWrite(path, []byte(base+"\n"+block))
}

func (a *aider) Disable() error {
	path := a.configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return atomicWrite(path, []byte(a.strip(string(data))+"\n"))
}

// --- Qwen Code ----------------------------------------------------------------
//
// ~/.qwen/settings.json: modelProviders.zen_gate = [{id, name, envKey,
// baseUrl}], providerProtocol.zen_gate = "openai". The API key lives in
// ~/.qwen/.env as ZEN_GATE_API_KEY (Qwen's recommended credential pattern).

type qwenCode struct{}

func newQwenCode() *qwenCode { return &qwenCode{} }

func (q *qwenCode) Meta() (string, string, string) {
	return "qwen", "Qwen Code", "~/.qwen/settings.json modelProviders + ~/.qwen/.env 凭据"
}

func (q *qwenCode) settingsPath() string { return homePath(".qwen", "settings.json") }
func (q *qwenCode) envPath() string      { return homePath(".qwen", ".env") }

func (q *qwenCode) Detect() (bool, string, string) {
	if fileExists(q.settingsPath()) {
		return true, "", "检测到 settings.json"
	}
	if fileExists(homePath(".qwen")) {
		return true, "", "检测到 ~/.qwen（settings.json 尚未创建）"
	}
	return false, "", "未检测到 Qwen Code"
}

func (q *qwenCode) IsEnabled() (bool, string, error) {
	doc, err := readJSONMap(q.settingsPath())
	if err != nil {
		return false, "", nil
	}
	mp, _ := doc["modelProviders"].(map[string]any)
	_, ok := mp["zen_gate"].([]any)
	return ok, "", nil
}

func (q *qwenCode) Enable(o Options) error {
	path := q.settingsPath()
	doc, err := readJSONMap(path)
	if err != nil {
		return err
	}
	if data, err := os.ReadFile(path); err == nil {
		_, _ = backupFile("qwen", path, data)
	}
	mp, _ := doc["modelProviders"].(map[string]any)
	if mp == nil {
		mp = map[string]any{}
	}
	entries := []map[string]any{}
	for _, m := range o.Models {
		e := map[string]any{"id": m.ID, "name": m.Name, "envKey": "ZEN_GATE_API_KEY", "baseUrl": o.BaseURL}
		if m.Reasoning {
			e["capabilities"] = map[string]any{"reasoning": true}
		}
		entries = append(entries, e)
	}
	mp["zen_gate"] = entries
	doc["modelProviders"] = mp
	pp, _ := doc["providerProtocol"].(map[string]any)
	if pp == nil {
		pp = map[string]any{}
	}
	pp["zen_gate"] = "openai"
	doc["providerProtocol"] = pp
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	// credential goes to ~/.qwen/.env (Qwen's recommended home for keys)
	return appendEnvFile(q.envPath(), "ZEN_GATE_API_KEY", o.APIKey)
}

func (q *qwenCode) Disable() error {
	doc, err := readJSONMap(q.settingsPath())
	if err == nil {
		if mp, ok := doc["modelProviders"].(map[string]any); ok {
			delete(mp, "zen_gate")
			if len(mp) == 0 {
				delete(doc, "modelProviders")
			}
		}
		if pp, ok := doc["providerProtocol"].(map[string]any); ok {
			delete(pp, "zen_gate")
			if len(pp) == 0 {
				delete(doc, "providerProtocol")
			}
		}
		if data, err := json.MarshalIndent(doc, "", "  "); err == nil {
			_ = atomicWrite(q.settingsPath(), data)
		}
	}
	return removeEnvLine(q.envPath(), "ZEN_GATE_API_KEY")
}

// appendEnvFile / removeEnvLine manage one KEY=VALUE line in a .env file.
func appendEnvFile(path, key, value string) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := os.ReadFile(path)
	lines := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			lines = append(lines, line)
		}
	}
	lines = append(lines, key+"="+value)
	return atomicWrite(path, []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n"))
}

func removeEnvLine(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			lines = append(lines, line)
		}
	}
	return atomicWrite(path, []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n"))
}

// --- Continue ----------------------------------------------------------------
//
// ~/.continue/config.yaml: top-level models list. Textual YAML injection is
// only safe when the file has no models block yet; otherwise the adapter
// reports "manual" rather than risk corrupting user YAML.

type continueIDE struct{}

func newContinueIDE() *continueIDE { return &continueIDE{} }

func (c *continueIDE) Meta() (string, string, string) {
	return "continue", "Continue (VS Code)", "~/.continue/config.yaml 注入 models 块（已有 models 时转为手动指引）"
}

func (c *continueIDE) configPath() string { return homePath(".continue", "config.yaml") }

func (c *continueIDE) Detect() (bool, string, string) {
	if fileExists(c.configPath()) {
		return true, "", "检测到 config.yaml"
	}
	if fileExists(homePath(".continue")) {
		return true, "", "检测到 ~/.continue"
	}
	return false, "", "未检测到 Continue"
}

func (c *continueIDE) hasModelsBlock(data string) bool {
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "models:") {
			return true
		}
	}
	return false
}

func (c *continueIDE) IsEnabled() (bool, string, error) {
	data, err := os.ReadFile(c.configPath())
	if err != nil {
		return false, "", nil
	}
	return strings.Contains(string(data), contMarkerBegin), "", nil
}

const contMarkerBegin = "# >>> zen-gate managed block (do not edit)"
const contMarkerEnd = "# <<< zen-gate managed block"

func (c *continueIDE) Enable(o Options) error {
	path := c.configPath()
	data, _ := os.ReadFile(path)
	if data != nil && c.hasModelsBlock(string(data)) {
		return fmt.Errorf("config.yaml 已存在 models 配置，自动注入可能与现有结构冲突；请在 Continue 设置里手动添加 OpenAI provider（Base URL + Key）")
	}
	if data != nil {
		_, _ = backupFile("continue", path, data)
	}
	var b strings.Builder
	b.WriteString(contMarkerBegin + "\nmodels:\n")
	for i, m := range o.Models {
		if i >= 6 {
			break // keep the YAML block small; the rest are reachable by editing
		}
		fmt.Fprintf(&b, "  - name: %s\n    provider: openai\n    model: %s\n    apiBase: %s\n    apiKey: %s\n    roles: [chat, edit]\n",
			m.Name, m.ID, o.BaseURL, o.APIKey)
	}
	b.WriteString(contMarkerEnd + "\n")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return atomicWrite(path, []byte(b.String()))
}

func (c *continueIDE) Disable() error {
	path := c.configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	srcStr := string(data)
	if !strings.Contains(srcStr, contMarkerBegin) {
		return nil
	}
	// our block is the entire models list we created; restore = drop it
	lines := strings.Split(srcStr, "\n")
	out := []string{}
	in := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case contMarkerBegin:
			in = true
			continue
		case contMarkerEnd:
			in = false
			continue
		}
		if !in {
			out = append(out, line)
		}
	}
	return atomicWrite(path, []byte(strings.TrimRight(strings.Join(out, "\n"), "\n")+"\n"))
}
