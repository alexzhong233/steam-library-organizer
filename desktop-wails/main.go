package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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

var guiURL = regexp.MustCompile(`GUI:\s+(https?://127\.0\.0\.1:[0-9]+/)`)

type App struct {
	ctx       context.Context
	core      *exec.Cmd
	coreURL   string
	coreError string
	mu        sync.Mutex
	stopOnce  sync.Once
}

func (a *App) startup(_ context.Context) {
	// The core is started before wails.Run so the embedded WebView can request
	// the URL immediately. This hook remains for future Wails bindings.
}

// CoreURL is called by the embedded frontend after Wails has initialised its
// bindings. The page itself stays in the core's origin, so all existing API
// calls and relative assets continue to work unchanged.
func (a *App) CoreURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.coreURL
}

func (a *App) CoreError() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.coreError
}

func (a *App) shutdown(_ context.Context) {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		core := a.core
		a.mu.Unlock()
		if core == nil || core.Process == nil {
			return
		}
		_ = core.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = core.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = core.Process.Kill()
		}
	})
}

func main() {
	core, err := startCore()
	if err != nil {
		panic(err)
	}
	app := &App{core: core.cmd, coreURL: core.url}
	defer app.shutdown(context.Background())
	target, err := url.Parse(core.url)
	if err != nil {
		panic(fmt.Errorf("核心 GUI 地址无效：%w", err))
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(w, "Steam 收藏核心连接失败："+proxyErr.Error(), http.StatusBadGateway)
	}

	err = wails.Run(&options.App{
		Title:     "Steam 收藏整理",
		Width:     1440,
		Height:    900,
		MinWidth:  980,
		MinHeight: 640,
		// The core serves the complete page and all /api routes. Proxying it
		// through Wails keeps one WebView origin, so review windows and relative
		// API calls work without an external browser or iframe.
		AssetServer:              &assetserver.Options{Handler: proxy},
		BackgroundColour:         options.NewRGB(15, 23, 32),
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		Bind:                     []interface{}{app},
		EnableDefaultContextMenu: true,
	})
	if err != nil {
		panic(err)
	}
}

type runningCore struct {
	cmd *exec.Cmd
	url string
}

func startCore() (runningCore, error) {
	corePath, err := locateCore()
	if err != nil {
		return runningCore{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return runningCore{}, err
	}
	dataDir := filepath.Join(home, ".steam-collection-organizer")
	if err = os.MkdirAll(dataDir, 0o755); err != nil {
		return runningCore{}, err
	}

	cmd := exec.Command(corePath, "-gui", "-data-dir", dataDir)
	cmd.Env = shimmedEnvironment()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return runningCore{}, err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return runningCore{}, fmt.Errorf("无法启动 Steam 收藏核心：%w", err)
	}
	urlCh := make(chan string, 1)
	go scanCoreOutput(stdout, urlCh)
	select {
	case u := <-urlCh:
		return runningCore{cmd: cmd, url: u}, nil
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		return runningCore{}, errors.New("Steam 收藏核心启动超时")
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
		return "", fmt.Errorf("STEAM_COLLECTION_CORE 不存在：%s", explicit)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(root, "Resources", "SteamCollectionImporterCore"),
		filepath.Join(root, "..", "Resources", "SteamCollectionImporterCore"),
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(root, "SteamCollectionImporterCore.exe"))
	}
	for _, candidate := range candidates {
		if isFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("找不到 SteamCollectionImporterCore；请将核心放在 Resources 目录，或设置 STEAM_COLLECTION_CORE")
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func shimmedEnvironment() []string {
	env := os.Environ()
	if runtime.GOOS != "darwin" {
		return env
	}
	shimDir, err := os.MkdirTemp("", "steam-collection-wails-open-")
	if err != nil {
		return env
	}
	// The temporary directory is intentionally retained for the process. The
	// core only needs this small no-op shim to avoid opening an external browser.
	shim := filepath.Join(shimDir, "open")
	_ = os.WriteFile(shim, []byte("#!/bin/sh\nprev=''\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"-a\" ] && [ \"$arg\" = \"Steam\" ]; then exec /usr/bin/open \"$@\"; fi\n  case \"$arg\" in steam://*) exec /usr/bin/open \"$@\";; esac\n  prev=\"$arg\"\ndone\nexit 0\n"), 0o755)
	for i, value := range env {
		if strings.HasPrefix(value, "PATH=") {
			env[i] = "PATH=" + shimDir + string(os.PathListSeparator) + strings.TrimPrefix(value, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+shimDir)
}
