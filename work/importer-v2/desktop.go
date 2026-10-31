//go:build desktop

// 桌面入口：同一个二进制既是核心又是窗口。
//
// 旧结构是「外壳 exe + Resources 里的 core exe」，靠解析 core 打印的
// `GUI: http://127.0.0.1:PORT/` 再做反向代理，产物必然是双文件、双进程。
// 这里反过来：runGUI 拿到 mux 后不监听端口（见 gui.go 的 desktopServe 分支），
// 直接把 handler 交给系统 WebView。于是
//   - Windows 只有一个 .exe；
//   - macOS 的 .app 里只有一个可执行文件，没有 Resources 子进程；
//   - 没有任何本地端口对外开放。
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	desktopShell = true

	home, err := desktopDataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法准备数据目录：", err)
		os.Exit(1)
	}
	m, source, err := loadConfiguredManifest("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取分类配置失败：", err)
		os.Exit(1)
	}

	handlers := make(chan http.Handler, 1)
	done := make(chan struct{})
	desktopServe = func(h http.Handler) { handlers <- h }
	desktopDone = done

	go func() {
		if err := runGUI(m, source, "", 0, home); err != nil {
			fmt.Fprintln(os.Stderr, "界面初始化失败：", err)
		}
	}()

	var root http.Handler
	select {
	case root = <-handlers:
	case <-time.After(30 * time.Second):
		root = failurePage("界面在 30 秒内没有就绪。多半是 Steam 没运行，或者数据目录不可写。")
	}

	var appCtx context.Context
	desktopQuit = func() {
		if appCtx != nil {
			wailsruntime.Quit(appCtx)
		}
	}

	app := &options.App{
		Title:            "Steam 收藏整理",
		Width:            1440,
		Height:           900,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: options.NewRGB(15, 23, 32),
		// 只给 Handler、不给 Assets：Wails 不会去找 frontend/dist，
		// 所有请求原样交给核心的路由。
		AssetServer:              &assetserver.Options{Handler: newShellRouter(root)},
		EnableDefaultContextMenu: true,
		OnStartup:                func(ctx context.Context) { appCtx = ctx },
	}
	if err = wails.Run(app); err != nil {
		fmt.Fprintln(os.Stderr, "窗口启动失败：", err)
		os.Exit(1)
	}
	close(done)
}

// desktopDataDir 放在用户主目录，而不是可执行文件旁边：
// 单文件分发时程序可能位于 /Applications 或只读目录，写不进去。
func desktopDataDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(h, ".steam-collection-organizer")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// shellRouter 处理首页路径。
//
// Wails 的 AssetServer 对 "/" 和 "*/index.html" 会把响应体截下来做 HTML 重排、
// 并注入 /wails/runtime.js；核心首页是一整页内嵌 SPA，没必要过这层解析器。
// 所以 "/" 302 到 "/ui"，"/ui" 再还原成 "/" 交给核心。前端用的都是相对路径，
// 换成 /ui 之后 fetch('/api/...') 仍然是同源请求。
type shellRouter struct {
	next http.Handler
}

func newShellRouter(next http.Handler) http.Handler {
	return &shellRouter{next: next}
}

func (s *shellRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "":
		http.Redirect(w, r, "/ui", http.StatusFound)
	case "/":
		http.Redirect(w, r, "/ui", http.StatusFound)
	case "/ui":
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		s.next.ServeHTTP(w, r2)
	default:
		s.next.ServeHTTP(w, r)
	}
}

func failurePage(message string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>启动失败</title>
<style>html,body{height:100%;margin:0;background:#0f1720;color:#e9f0f5;font:15px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
main{max-width:720px;margin:0 auto;padding:48px 24px}h1{font-size:22px}code{display:block;white-space:pre-wrap;background:#182635;border:1px solid #2c4050;border-radius:8px;padding:14px;margin:16px 0;line-height:1.6}
p{color:#9eb0bb;line-height:1.7}</style></head><body><main>
<h1>Steam 收藏整理没能启动</h1><code>`+escapeHTML(message)+`</code>
<p>先确认 Steam 正在运行并已登录，然后重新打开本程序。</p>
<p>需要看详细日志时，用同一个程序加 <code style="display:inline">-gui</code> 从命令行启动即可。</p>
</main></body></html>`)
	})
}

func escapeHTML(s string) string {
	r := make([]rune, 0, len(s))
	for _, c := range s {
		switch c {
		case '<':
			r = append(r, '&', 'l', 't', ';')
		case '>':
			r = append(r, '&', 'g', 't', ';')
		case '&':
			r = append(r, '&', 'a', 'm', 'p', ';')
		default:
			r = append(r, c)
		}
	}
	return string(r)
}
