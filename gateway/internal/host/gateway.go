// gateway.go —— 把内嵌网关接进 UI 的数据层。
//
// 网关本身（免费车道 + OpenAI/Anthropic 兼容 API + Agent 配置注入 + 用量/日志）
// 是一套独立后端。这里只做一件事：把页面发来的请求原样喂给网关真正的管理路由，
// 再把结果原样回传。不复制任何业务逻辑，所以页面看到的每个数字都和 API 使用者
// 看到的一致。

package host

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// isFailureStatus reports whether the gateway answered with a client/server
// fault. Its admin routes put {"ok":false} in most error bodies, but a 404
// ("not found") and a 500 panic do not, and a page that only reads the body
// would show those as success.
func isFailureStatus(code int) bool { return code >= 400 }

// gatewayErrorText pulls the most useful sentence out of an error body: the
// gateway's own "error" string when there is one, its raw text otherwise.
func gatewayErrorText(body []byte, status int) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err == nil {
		if s, isStr := m["error"].(string); isStr && s != "" {
			return s
		}
	}
	if text := strings.TrimSpace(string(body)); text != "" && len(text) < 400 {
		return text
	}
	return fmt.Sprintf("网关返回 %d", status)
}

// handleGateway is the page's only door into the gateway.
//
//	{"path":"state"}                  → GET  /admin/api/state
//	{"path":"settings","body":{...}} → POST /admin/api/settings
//
// The gateway's JSON is passed through as data; the page does not re-interpret
// it, so there is exactly one source of truth for every number shown.
func (c *Center) handleGateway(raw json.RawMessage) *Envelope {
	b := c.gateway
	if b == nil {
		return fail(CodeGatewayDown, "网关没有启动")
	}

	var args struct {
		Path string          `json:"path"`
		Body json.RawMessage `json:"body"`
		// 导出类路由(export / usage.csv / logs/download)回答的不是 JSON 而是
		// 附件：SaveTo 把那份字节落到用户选的文件；LoadFrom 反过来把 import
		// 的请求体从磁盘读上来。网关只认流，不认本机路径，所以文件这一步在这里。
		SaveTo   string `json:"saveTo"`
		LoadFrom string `json:"loadFrom"`
	}
	if len(raw) == 0 || string(raw) == "null" {
		return fail(CodeBadPayload, "缺少 path")
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return fail(CodeBadPayload, "参数不是合法 JSON："+err.Error())
	}
	if args.Path == "" {
		return fail(CodeBadPayload, "缺少 path")
	}

	var body any
	if len(args.Body) > 0 && string(args.Body) != "null" {
		if err := json.Unmarshal(args.Body, &body); err != nil {
			return fail(CodeBadPayload, "body 不是合法 JSON："+err.Error())
		}
	}
	if args.LoadFrom != "" {
		// 导入配置：文件里就是网关要的请求体。读盘和解析在这里做，校验仍归
		// 网关的 import 路由，所以"不是合法配置文件"仍然由它那句话报出来。
		fileRaw, err := os.ReadFile(args.LoadFrom)
		if err != nil {
			return fail(CodeIOError, "读取文件失败："+err.Error())
		}
		if err := json.Unmarshal(fileRaw, &body); err != nil {
			return fail(CodeParseFailed, "文件不是合法 JSON："+err.Error())
		}
	}

	status, out, err := b.AdminRequest(args.Path, body)
	if err != nil {
		return fail(CodeGatewayDown, "网关调用失败："+err.Error())
	}
	if isFailureStatus(status) {
		// 404/500 这类回答不带 ok 字段，只看 {"ok":} 会让页面把"路由不存在"
		// 当成成功——这正是最容易假装的地方，所以按状态码先拦一道。
		return fail(CodeGatewayRejected, gatewayErrorText(out, status))
	}

	if args.SaveTo != "" {
		// 导出：网关回的是附件字节，原样落盘，不经过 JSON 再拼一遍。
		if err := os.WriteFile(args.SaveTo, out, 0o644); err != nil {
			return fail(CodeIOError, "写入文件失败："+err.Error())
		}
		return ok(map[string]any{"path": args.SaveTo, "bytes": len(out)})
	}

	var decoded any
	if err := json.Unmarshal(out, &decoded); err != nil {
		// 非 JSON 响应（导出、CSV）原样回文本。
		decoded = string(out)
	}

	// 网关用 {"ok":false,"error":...} 表达业务失败；翻译成统一信封，
	// 页面只看一层 ok。
	if m, isMap := decoded.(map[string]any); isMap {
		if okv, has := m["ok"]; has {
			if vb, _ := okv.(bool); !vb {
				return fail(CodeGatewayRejected, gatewayErrorText(out, status))
			}
		}
	}

	return ok(decoded)
}
