// Command devserver runs the Android UI at a phone viewport, in a browser, backed
// by the real gateway.
//
// The APK gets its data through the Kotlin bridge -> mobile.Admin. A browser has
// no such bridge, so this command supplies that one missing piece and nothing
// else:
//
//	window.chrome.webview.postMessage  ->  POST /dev/api  ->  the same in-process
//	                                         admin router the APK calls
//
// Everything else - assets/ui/index.html, the Go gateway, the real admin
// handlers - is exactly what ships. dev-host.js is never copied into the APK;
// it exists so the layout can be judged with real numbers instead of a mock.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"zenui/internal/zen/core"
	"zenui/internal/zenbridge"
)

const indexInject = `<script src="/dev-host.js"></script>`

func main() {
	uiDir := flag.String("ui", filepath.Join("..", "..", "android", "app", "src", "main", "assets", "ui"), "assets/ui directory to serve")
	home := flag.String("home", "", "gateway data dir (default: a temp dir)")
	addr := flag.String("addr", "127.0.0.1:8099", "listen address")
	flag.Parse()

	if *home == "" {
		dir, err := os.MkdirTemp("", "lamp-dev-")
		if err != nil {
			log.Fatal(err)
		}
		*home = filepath.Join(dir, "zen-gate")
	}

	c, err := core.Start(context.Background(), core.Options{
		Home:           *home,
		Port:           8787,
		NoTray:         true,
		DisableDesktop: true,
		// The preview host runs on a machine whose ~/.config belongs to the
		// real agents. Without this, opening the Agent page re-injects *this*
		// gateway's endpoint and a throwaway key into the person's own
		// ~/.dsh profiles — observed the first time the page was opened.
		ReadOnlyAgents: true,
	})
	if err != nil {
		log.Fatalf("网关启动失败: %v", err)
	}
	defer c.Stop()

	bridge := zenbridge.New(c.Gateway)

	absUI, err := filepath.Abs(*uiDir)
	if err != nil {
		log.Fatal(err)
	}
	files := http.FileServer(http.Dir(absUI))

	mux := http.NewServeMux()
	mux.HandleFunc("/dev/api", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "读请求失败", http.StatusBadRequest)
			return
		}
		var req struct {
			ID      int             `json:"id"`
			Method  string          `json:"method"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "请求不是合法 JSON", http.StatusBadRequest)
			return
		}
		if req.Method != "Gateway" {
			// The four native actions have no browser implementation: answer
			// honestly so the page draws its InfoBar instead of pretending.
			writeReply(w, req.ID, false, nil, "浏览器预览里没有这个宿主动作")
			return
		}
		var args struct {
			Path string          `json:"path"`
			Body json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(req.Payload, &args); err != nil {
			writeReply(w, req.ID, false, nil, "参数不合法："+err.Error())
			return
		}
		var body any
		if len(args.Body) > 0 && string(args.Body) != "null" {
			if err := json.Unmarshal(args.Body, &body); err != nil {
				writeReply(w, req.ID, false, nil, "body 不是合法 JSON")
				return
			}
		}
		status, out, err := bridge.AdminRequest(args.Path, body)
		if err != nil {
			writeReply(w, req.ID, false, nil, err.Error())
			return
		}
		if status >= 400 {
			writeReply(w, req.ID, false, nil, fmt.Sprintf("网关返回 %d", status))
			return
		}
		writeReply(w, req.ID, true, out, "")
	})

	mux.HandleFunc("/dev-host.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/javascript; charset=utf-8")
		_, _ = w.Write([]byte(devHostJS))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "/index.html") {
			serveIndex(w, filepath.Join(absUI, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})

	log.Printf("开发预览: http://%s/  (网关 %s)", *addr, c.BaseURL())
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func writeReply(w http.ResponseWriter, id int, ok bool, data json.RawMessage, errText string) {
	msg := map[string]any{"id": id, "ok": ok}
	if len(data) > 0 {
		msg["data"] = data
	}
	if errText != "" {
		msg["error"] = errText
	}
	w.Header().Set("content-type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(msg)
}

// serveIndex injects the browser host into the shipping page; the APK never
// gets this byte.
func serveIndex(w http.ResponseWriter, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "读不到 index.html", http.StatusNotFound)
		return
	}
	body := strings.Replace(string(raw), "</head>", indexInject+"</head>", 1)
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, body)
}

// devHostJS is the browser stand-in for the Kotlin bridge: same protocol, same
// envelope, so bridge.js and every page run unmodified.
const devHostJS = `
(function () {
  var pending = new Map();
  window.__hostReceive = function (raw) {
    try { window.dispatchMessage && 0; } catch (e) {}
    var msg = JSON.parse(raw);
    var fn = pending.get(msg.id);
    if (fn) { pending.delete(msg.id); fn(msg); }
  };
  window.chrome = window.chrome || {};
  window.chrome.webview = {
    postMessage: function (raw) {
      var req = JSON.parse(String(raw));
      fetch("/dev/api", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(req)
      }).then(function (r) { return r.json(); }).then(function (msg) {
        window.__hostReceive(JSON.stringify(msg));
      }).catch(function (err) {
        window.__hostReceive(JSON.stringify({ id: req.id, ok: false, error: "预览宿主不可达：" + err }));
      });
    }
  };
})();
`
