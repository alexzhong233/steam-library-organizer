//go:build desktop

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 桌面模式有三条不能改的约定，改错了表现是"窗口全白"或"审核窗口点了没反应"。
// 这里把三条都钉住。

// 约定一："/" 必须 302 到 "/ui"。
// Wails 的 AssetServer 看到 "/" 会把响应体截下来做 HTML 重排并注入 runtime.js，
// 我们的首页是一整页内嵌 SPA，不能过那层解析器。
func TestShellRouterRedirectsRootToUI(t *testing.T) {
	called := false
	h := newShellRouter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("GET / 应为 302，实际 %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/ui" {
		t.Fatalf("GET / 应跳到 /ui，实际 %q", loc)
	}
	if called {
		t.Fatal("GET / 不应交给核心处理")
	}
}

// 约定二："/ui" 要还原成 "/" 再交给核心，否则核心的 "/" 路由收不到。
func TestShellRouterMapsUIToRoot(t *testing.T) {
	var gotPath string
	h := newShellRouter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ui", nil))
	if gotPath != "/" {
		t.Fatalf("/ui 应还原成 /，实际 %q", gotPath)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", w.Code)
	}
}

// 约定三：其余路径（/api/*、/review）必须原样透传，前端全是相对路径。
func TestShellRouterPassesThroughAPIAndReview(t *testing.T) {
	for _, path := range []string{"/api/state", "/review", "/api/history", "/api/save-json"} {
		var got string
		h := newShellRouter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if got != path {
			t.Fatalf("%s 被改写成 %q", path, got)
		}
		if w.Code == http.StatusFound {
			t.Fatalf("%s 不该被重定向", path)
		}
	}
}

// 桌面模式要在首页注入标记：前端据此知道 window.open 不可用，
// 改用同源内嵌浮层打开审核窗口。浏览器模式不能有这个标记。
func TestGUIPageDesktopFlag(t *testing.T) {
	old := desktopShell
	defer func() { desktopShell = old }()

	desktopShell = false
	// 页面脚本里本来就有读取该标记的代码，所以只能比对赋值语句。
	if strings.Contains(guiPage(), "window.__desktopShell=true") {
		t.Fatal("浏览器模式不该注入 window.__desktopShell=true")
	}

	desktopShell = true
	page := guiPage()
	if !strings.Contains(page, "window.__desktopShell=true") {
		t.Fatal("桌面模式必须注入 window.__desktopShell=true")
	}
	if !strings.Contains(page, "</body>") || !strings.Contains(page, "</html>") {
		t.Fatal("注入破坏了页面结构")
	}
	if strings.Count(page, "<html") != 1 {
		t.Fatal("注入后页面被复制了一份")
	}
}

func TestFailurePageIsReadable(t *testing.T) {
	w := httptest.NewRecorder()
	failurePage("Steam 未运行 <坏>").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	body, _ := io.ReadAll(w.Body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("失败页应为 503，实际 %d", w.Code)
	}
	s := string(body)
	if !strings.Contains(s, "Steam 未运行 &lt;坏&gt;") {
		t.Fatalf("失败信息应转义后展示，实际：%s", s)
	}
}
