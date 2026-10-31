package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// coreSpy 记录核心真正收到了什么，用来验证反代有没有改坏请求。
type coreSpy struct {
	mu     sync.Mutex
	hits   int
	origin string
	path   string
	method string
}

func (s *coreSpy) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits++
	s.origin = r.Header.Get("Origin")
	s.path = r.URL.Path
	s.method = r.Method
}

func (s *coreSpy) snapshot() (int, string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits, s.origin, s.path, s.method
}

// fakeCore 模拟核心的 GUI：首页是内嵌 HTML，/api/* 走同源 fetch。
func fakeCore(t *testing.T) (*httptest.Server, *coreSpy) {
	t.Helper()
	spy := &coreSpy{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spy.record(r)
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, "<!doctype html><html><head><title>core</title></head><body>core</body></html>")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"path":"`+r.URL.Path+`","method":"`+r.Method+`"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, spy
}

func TestRootRedirectsToMainPath(t *testing.T) {
	core, _ := fakeCore(t)
	handler := newShellHandler(core.URL + "/")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d，期望 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != mainPath {
		t.Fatalf("Location = %q，期望 %q", loc, mainPath)
	}
}

// 内嵌 WebView 的 origin 一定不是核心的 origin，而核心的 guiGuard 只认自己，
// 所以反代必须改写 Origin，否则首页之后每个 POST 都会被 403。
func TestMainPageProxiedAndFlagInjected(t *testing.T) {
	core, spy := fakeCore(t)
	handler := newShellHandler(core.URL + "/")

	req := httptest.NewRequest(http.MethodGet, mainPath, nil)
	req.Header.Set("Origin", "http://wails.localhost")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	flagAt := strings.Index(body, "window.__desktopShell=true")
	if flagAt < 0 {
		t.Fatalf("首页没有注入 __desktopShell 标记：%s", body)
	}
	if headAt := strings.Index(body, "</head>"); headAt >= 0 && flagAt > headAt {
		t.Fatalf("标记应插在 </head> 之前：%s", body)
	}
	if got := rec.Header().Get("Content-Length"); got != "" && got != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q，与改写后的正文长度 %d 不一致", got, len(body))
	}

	hits, origin, path, _ := spy.snapshot()
	if hits != 1 || path != "/" {
		t.Fatalf("核心收到 hits=%d path=%q，期望把 %q 映射成 /", hits, path, mainPath)
	}
	if want := core.URL; origin != want {
		t.Fatalf("核心收到的 Origin = %q，期望 %q", origin, want)
	}
}

func TestAPIPostKeepsMethodAndRewritesOrigin(t *testing.T) {
	core, spy := fakeCore(t)
	handler := newShellHandler(core.URL + "/")

	req := httptest.NewRequest(http.MethodPost, "/api/state", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://wails.localhost")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"method":"POST"`) || !strings.Contains(body, `"path":"/api/state"`) {
		t.Fatalf("核心没有收到原样的请求：%s", body)
	}
	if strings.Contains(body, "__desktopShell") {
		t.Fatalf("非首页不该注入标记：%s", body)
	}
	if _, origin, path, method := spy.snapshot(); method != http.MethodPost || path != "/api/state" {
		t.Fatalf("核心收到 %s %s，期望 POST /api/state", method, path)
	} else if origin != core.URL {
		t.Fatalf("核心收到的 Origin = %q，期望 %q", origin, core.URL)
	}
}

func TestReviewPageNotRewritten(t *testing.T) {
	core, spy := fakeCore(t)
	handler := newShellHandler(core.URL + "/")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/review?wait=1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if _, _, path, _ := spy.snapshot(); path != "/review" {
		t.Fatalf("核心收到路径 %q，期望 /review", path)
	}
}

func TestCoreUnreachableShowsFailurePage(t *testing.T) {
	handler := newShellHandler("http://127.0.0.1:1/")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, mainPath, nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Steam 收藏整理没能启动") {
		t.Fatalf("没有给出可读的失败页：%s", rec.Body.String())
	}
}

func TestInvalidCoreURLShowsFailurePage(t *testing.T) {
	handler := newShellHandler("not-a-url")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, mainPath, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d，期望 503", rec.Code)
	}
}
