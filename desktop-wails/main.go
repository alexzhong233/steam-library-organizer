package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// 外壳不自己画界面，只做三件事：拉起核心、把核心的网页反代进系统 WebView、退出时收掉核心。
//
// 为什么是反代而不是 iframe：
//   - macOS 上 Wails 页面跑在 wails:// 自定义协议里，往 http://127.0.0.1 的 iframe 属于混合内容，
//     WKWebView 默认会拦；反代之后整页只有一个 origin，不存在这个问题。
//   - 页面里的 fetch('/api/...') 全程相对路径，反代后依然是同源请求，
//     核心那套 Host/Origin 校验不用放开，安全性跟直接用浏览器一模一样。

var guiURL = regexp.MustCompile(`GUI:\s+(https?://127\.0\.0\.1:[0-9]+/)`)

// mainPath 是外壳把首页挂上去的路径。
// 不能用 "/"：Wails 的 AssetServer 对 "/" 和 "*/index.html" 会先把响应体截下来做 HTML 重排
// 并注入 /wails/runtime.js。核心的首页是整页内嵌的 SPA，没必要让它过一遍 HTML 解析器。
const mainPath = "/ui"

type shellHandler struct {
	proxy  *httputil.ReverseProxy
	errMsg string
}

func newShellHandler(coreURL string) *shellHandler {
	target, err := url.Parse(coreURL)
	if err != nil || target.Host == "" {
		return &shellHandler{errMsg: "核心返回的 GUI 地址无效：" + coreURL}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		main := r.URL.Path == mainPath
		if main {
			r.URL.Path = "/"
		}
		director(r)
		// 核心只认自己的 origin（Origin 头必须等于 http://<自己的 Host>），
		// 浏览器发过来的一定是 wails.localhost / wails://，这里统一改写，其余一律照样透传。
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", "http://"+target.Host)
		}
		if main {
			*r = *r.WithContext(context.WithValue(r.Context(), mainPageKey{}, true))
		}
	}
	// 核心有长任务接口，别让反代攒够 32KB 才吐。
	proxy.FlushInterval = -1
	proxy.ModifyResponse = injectDesktopFlag
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		writeFailurePage(w, "核心没有响应："+err.Error())
	}
	return &shellHandler{proxy: proxy}
}

type mainPageKey struct{}

// 首页里塞一个标记，前端据此知道自己在系统 WebView 里：
// 审核窗口改用内嵌浮层（WebView2 / WKWebView 都不支持 window.open 开新窗），
// 浏览器里这个标记不存在，行为跟以前完全一致。
func injectDesktopFlag(resp *http.Response) error {
	if resp.Request == nil || resp.Request.Context().Value(mainPageKey{}) == nil {
		return nil
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	flag := []byte(`<script>window.__desktopShell=true</script>`)
	if bytes.Contains(body, []byte("</head>")) {
		body = bytes.Replace(body, []byte("</head>"), append(flag, []byte("</head>")...), 1)
	} else {
		body = append(flag, body...)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
	resp.Header.Del("Content-Encoding")
	return nil
}

func (h *shellHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		writeFailurePage(w, h.errMsg)
		return
	}
	if r.URL.Path == "/" && r.Method == http.MethodGet {
		http.Redirect(w, r, mainPath, http.StatusFound)
		return
	}
	h.proxy.ServeHTTP(w, r)
}

func writeFailurePage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>启动失败</title>
<style>html,body{height:100%;margin:0;background:#0f1720;color:#e9f0f5;font:15px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
main{max-width:720px;margin:0 auto;padding:48px 24px}h1{font-size:22px}code{display:block;white-space:pre-wrap;background:#182635;border:1px solid #2c4050;border-radius:8px;padding:14px;margin:16px 0;line-height:1.6}
p{color:#9eb0bb;line-height:1.7}a{color:#6bc4ef}</style></head><body><main>
<h1>Steam 收藏整理没能启动</h1><code>`+html.EscapeString(message)+`</code>
<p>常见原因：Steam 没有运行、核心文件缺失、或者杀毒软件拦下了核心进程。</p>
<p>可以直接用命令行启动核心看详细日志：</p>
<code>SteamCollectionImporterCore -gui</code>
<p>核心启动后会打印形如 <code style="display:inline">GUI: http://127.0.0.1:PORT/</code> 的地址，用浏览器打开也能正常使用。</p>
</main></body></html>`)
}

type runningCore struct {
	cmd  *exec.Cmd
	url  string
	once sync.Once
}

func (c *runningCore) stop() {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return
	}
	c.once.Do(func() {
		_ = c.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = c.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = c.cmd.Process.Kill()
		}
	})
}

func main() {
	core, coreErr := startCore()
	if core != nil {
		defer core.stop()
	}

	shell := &shellHandler{}
	if coreErr == nil {
		shell = newShellHandler(core.url)
	} else {
		shell.errMsg = coreErr.Error()
	}

	app := &options.App{
		Title:            "Steam 收藏整理",
		Width:            1440,
		Height:           900,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: options.NewRGB(15, 23, 32),
		// 只给 Handler：不传 Assets，Wails 就不会去找 frontend/dist，
		// 也不会对首页做 HTML 重排，所有请求原样交给核心。
		AssetServer:              &assetserver.Options{Handler: shell},
		OnShutdown:               func(context.Context) { core.stop() },
		EnableDefaultContextMenu: true,
	}
	if err := wails.Run(app); err != nil {
		fmt.Fprintln(os.Stderr, "窗口启动失败：", err)
		os.Exit(1)
	}
}

func startCore() (*runningCore, error) {
	corePath, err := locateCore()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(home, ".steam-collection-organizer")
	if err = os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	// -browser=false：外壳自己就是窗口，不要再弹一个默认浏览器标签页。
	cmd := exec.Command(corePath, "-gui", "-browser=false", "-data-dir", dataDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("无法启动核心 %s：%w", corePath, err)
	}
	core := &runningCore{cmd: cmd}

	urlCh := make(chan string, 1)
	go scanCoreOutput(stdout, urlCh)
	select {
	case u := <-urlCh:
		core.url = u
		return core, nil
	case <-time.After(20 * time.Second):
		core.stop()
		return nil, errors.New("核心 20 秒内没有打印 GUI 地址（通常是 Steam 未运行或端口被占用）")
	}
}

func scanCoreOutput(r io.Reader, out chan<- string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
		match := guiURL.FindStringSubmatch(line)
		if len(match) == 2 {
			if parsed, err := url.Parse(match[1]); err == nil && parsed.Hostname() == "127.0.0.1" {
				select {
				case out <- match[1]:
				default:
				}
			}
		}
	}
}

func locateCore() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("STEAM_COLLECTION_CORE")); explicit != "" {
		if isFile(explicit) {
			return explicit, nil
		}
		return "", fmt.Errorf("STEAM_COLLECTION_CORE 指向的文件不存在：%s", explicit)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(exe)
	candidates := []string{
		// macOS：.app/Contents/MacOS/App → Contents/Resources/Core
		filepath.Join(root, "..", "Resources", "SteamCollectionImporterCore"),
		// Windows：与外壳同级的 Resources 目录
		filepath.Join(root, "Resources", "SteamCollectionImporterCore"),
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(root, "Resources", "SteamCollectionImporterCore.exe"))
	}
	for _, candidate := range candidates {
		if isFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("找不到 SteamCollectionImporterCore，请确认它和本程序一起解压（%s）", root)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
