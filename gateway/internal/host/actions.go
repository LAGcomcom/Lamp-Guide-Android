// actions.go —— 原生外壳动作：文件对话框、打开位置、打开链接。
//
// 这些都是网关的管理路由做不到的事（它只认流，不认本机路径），所以留在宿主
// 这一侧。每个动作都如实回话：用户取消就是 canceled，找不到就是找不到。

package host

import (
	"encoding/json"
	"os"
	"strings"
)

func (c *Center) handleOpenPath(raw json.RawMessage) *Envelope {
	args, bad := unmarshalArgs[struct {
		Path string `json:"path"`
	}](raw)
	if bad != nil {
		return bad
	}
	target := strings.TrimSpace(expandEnv(args.Path))
	if target == "" {
		return fail(CodeBadPayload, "缺少 path")
	}
	if _, err := os.Stat(target); err != nil {
		return fail(CodeIOError, "找不到这个位置："+target)
	}
	if err := OpenInShell(target); err != nil {
		return fail(CodeIOError, "无法打开位置："+err.Error())
	}
	return ok(map[string]any{"path": target})
}

func (c *Center) handleOpenURL(raw json.RawMessage) *Envelope {
	args, bad := unmarshalArgs[struct {
		URL string `json:"url"`
	}](raw)
	if bad != nil {
		return bad
	}
	target := strings.TrimSpace(args.URL)
	lower := strings.ToLower(target)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fail(CodeInvalidValue, "只接受 http 或 https 地址")
	}
	if err := OpenURL(target); err != nil {
		return fail(CodeIOError, "无法打开链接："+err.Error())
	}
	return ok(map[string]any{"url": target})
}

// handlePickSavePath runs the native save dialog and returns the chosen path
// without writing anything. 网关页的导出类路由需要它：字节由网关给，目的地由
// 用户选。
func (c *Center) handlePickSavePath(raw json.RawMessage) *Envelope {
	args, bad := unmarshalArgs[struct {
		Prompt string `json:"prompt"`
		Name   string `json:"name"`
	}](raw)
	if bad != nil {
		return bad
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		prompt = "保存文件"
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = "untitled"
	}
	path, canceled, err := PickSaveFile(prompt, name)
	if err != nil {
		return fail(CodeIOError, "无法打开保存对话框："+err.Error())
	}
	if canceled || path == "" {
		return ok(map[string]any{"canceled": true})
	}
	return ok(map[string]any{"canceled": false, "path": path})
}

// handlePickOpenPath is the matching open dialog, used by config import and by
// the subscription file flows.
func (c *Center) handlePickOpenPath(raw json.RawMessage) *Envelope {
	args, bad := unmarshalArgs[struct {
		Prompt string `json:"prompt"`
	}](raw)
	if bad != nil {
		return bad
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		prompt = "打开文件"
	}
	path, canceled, err := PickFile(prompt, nil)
	if err != nil {
		return fail(CodeIOError, "无法打开文件对话框："+err.Error())
	}
	if canceled || path == "" {
		return ok(map[string]any{"canceled": true})
	}
	return ok(map[string]any{"canceled": false, "path": path})
}

// expandEnv expands %VAR% style names so a stored path can be re-opened after
// the environment changed.
func expandEnv(p string) string {
	return os.ExpandEnv(strings.TrimSpace(p))
}
