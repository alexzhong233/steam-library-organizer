package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type guiState struct {
	startupDone  bool
	startupError string
	mu           sync.Mutex
	manifest     Manifest
	configSource string
	planPath     string
	port         int
	home         string
	root         string
	eval         Evaluator
	snapshot     Snapshot
	preview      *Preview
	prices       map[int]PriceInfo
	aiURL, aiKey string
	draft        *Manifest
	draftKind    string
	draftSource  string
	removal      *RemovalPlan
}

func normalizeAIBase(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if u, err := url.Parse(raw); err == nil && strings.Trim(u.Path, "/") != "" {
		return raw
	}
	return raw + "/v1"
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func readJSON(r *http.Request, v any) error {
	b, e := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func runSelfTest() error {
	m := Manifest{Schema: 2, GameCount: 3, Games: []Game{
		{AppID: 1, Name: "Synthetic One", Target: "One"},
		{AppID: 2, Name: "Synthetic Two", Target: "Two"},
		{AppID: 3, Name: "Synthetic Three", Target: "Three"},
	}}
	if e := validateManifest(m); e != nil {
		return e
	}
	if len(m.Games) < 3 {
		return fmt.Errorf("自测需要至少三款游戏")
	}
	test := m
	test.Games = append([]Game(nil), m.Games[:3]...)
	test.GameCount = len(test.Games)
	test.Games[0].Target, test.Games[0].LowValue = "低价值游戏", true
	test.Games[0].QualityIndie, test.Games[0].Protect, test.Games[0].FreeCandidate = false, false, false
	test.Games[0].KeepPrimary, test.Games[0].DynamicOnlyIfMatched = false, true
	test.Games[1].Target, test.Games[1].QualityIndie = "04｜精品小游戏", true
	test.Games[1].LowValue, test.Games[1].Protect, test.Games[1].FreeCandidate = false, false, false
	test.Games[1].KeepPrimary, test.Games[1].DynamicOnlyIfMatched = false, true
	test.Games[2].Target, test.Games[2].KeepPrimary = "自测｜重要系列", true
	test.Games[2].LowValue, test.Games[2].QualityIndie, test.Games[2].FreeCandidate = false, false, false
	test.Games[2].Protect, test.Games[2].DynamicOnlyIfMatched = false, true
	s := Snapshot{Schema: 1, Account: "self-test"}
	for _, g := range test.Games {
		s.Apps = append(s.Apps, App{AppID: g.AppID, Name: g.Name})
	}
	s.Collections = append(s.Collections, Collection{ID: "user-dynamic-test", Name: "用户自定义动态收藏", Dynamic: true, AppIDs: []int{test.Games[0].AppID, test.Games[1].AppID}})
	p := buildPreview(test, s, false)
	if p.ManifestCount != len(test.Games) || p.ScopeCount != len(test.Games) {
		return fmt.Errorf("合成预览数量错误")
	}
	if len(p.Blocked) > 0 {
		return fmt.Errorf("预览预检失败：%s", strings.Join(p.Blocked, "；"))
	}
	if p.Targets[test.Games[0].AppID] != "低价值游戏" || p.Targets[test.Games[1].AppID] != "04｜精品小游戏" || p.Targets[test.Games[2].AppID] != "自测｜重要系列" {
		return fmt.Errorf("动态命中时静态主收藏保护规则失败")
	}
	for _, action := range p.Actions {
		if action.Overlay {
			return fmt.Errorf("自测发现动态收藏写入动作")
		}
	}
	return nil
}
func runGUI(m Manifest, source, plan string, port int, report string) error {
	root, _ := steamRoot()
	if report == "" {
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		report = toolDirectory(executable)
	}
	absoluteReport, err := filepath.Abs(report)
	if err != nil {
		return err
	}
	report = absoluteReport
	if e := os.MkdirAll(report, 0700); e != nil {
		return e
	}
	st := &guiState{manifest: Manifest{Schema: 2, Games: []Game{}}, configSource: source, planPath: plan, port: port, home: report, root: root, prices: loadPrices(pricePath(report))}
	if e := os.MkdirAll(historyRoot(report), 0700); e != nil {
		return fmt.Errorf("无法创建工具旁的历史目录，请将工具移到可写目录或用 -data-dir 指定目录：%w", e)
	}
	var server *http.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, guiHTML)
	})
	mux.HandleFunc("/review", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, reviewHTML)
	})
	mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		items, err := listHistory(st.home)
		jsonResponse(w, map[string]any{"ok": err == nil, "error": errText(err), "items": items})
	})
	mux.HandleFunc("/api/history/delete", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID string `json:"id"`
		}
		if err := readJSON(r, &q); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		err := deleteHistory(st.home, q.ID)
		jsonResponse(w, map[string]any{"ok": err == nil, "error": errText(err)})
	})
	mux.HandleFunc("/api/history/read", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID string `json:"id"`
		}
		if err := readJSON(r, &q); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		items, err := listHistory(st.home)
		if err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		var selected *HistoryItem
		for i := range items {
			if items[i].ID == q.ID {
				selected = &items[i]
				break
			}
		}
		if selected == nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "历史记录不存在"})
			return
		}
		b, err := os.ReadFile(selected.Path)
		if err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		var data any
		if err = json.Unmarshal(b, &data); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "item": selected, "data": data})
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		jsonResponse(w, map[string]any{"history_dir": historyRoot(st.home), "startup_done": st.startupDone, "startup_error": st.startupError, "version": version, "config_source": st.configSource, "games": len(st.manifest.Games), "manifest": st.manifest, "overlays": st.manifest.Overlays, "connected": st.eval != nil, "snapshot": st.snapshot, "prices": st.prices, "preview": st.preview, "draft": st.draft, "draft_kind": st.draftKind, "draft_source": st.draftSource})
	})
	mux.HandleFunc("/api/import/normalize", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := readJSON(r, &raw); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		m, err := normalizeImport(raw, st.snapshot)
		jsonResponse(w, map[string]any{"ok": err == nil, "error": errText(err), "manifest": m})
	})
	mux.HandleFunc("/api/draft", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Manifest Manifest `json:"manifest"`
			Kind     string   `json:"kind"`
			Source   string   `json:"source"`
		}
		if err := readJSON(r, &q); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := validateManifest(q.Manifest); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		st.mu.Lock()
		cp := q.Manifest
		st.draft = &cp
		st.draftKind = q.Kind
		st.draftSource = q.Source
		st.mu.Unlock()
		jsonResponse(w, map[string]any{"ok": true, "manifest": q.Manifest, "kind": q.Kind, "source": q.Source})
	})
	mux.HandleFunc("/api/draft/accept", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.draft == nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "没有待审核草稿"})
			return
		}
		st.manifest = *st.draft
		st.configSource = "已接受草稿 · " + st.draftKind
		st.preview = nil
		st.draft = nil
		st.draftKind = ""
		st.draftSource = ""
		jsonResponse(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/draft/reject", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.draft = nil
		st.draftKind = ""
		st.draftSource = ""
		st.mu.Unlock()
		jsonResponse(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		steps := diagnoseConnection(st.port, st.home)
		ok := true
		for _, step := range steps {
			ok = ok && step.OK
		}
		jsonResponse(w, map[string]any{"ok": ok, "steps": steps})
	})
	mux.HandleFunc("/api/removal/preview", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			AppIDs []int `json:"appids"`
		}
		if err := readJSON(r, &q); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		st.removal = nil
		if st.eval == nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先连接 Steam"})
			return
		}
		eng := Engine{Eval: st.eval, Root: st.root, Home: st.home}
		p, err := eng.CheckRemoval(r.Context(), q.AppIDs, st.manifest, storeFetcherFor(r.Context(), q.AppIDs))
		if err == nil {
			st.removal = p
		}
		jsonResponse(w, map[string]any{"ok": err == nil, "error": errText(err), "plan": p})
	})
	mux.HandleFunc("/api/removal/apply", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Token   string `json:"token"`
			Confirm string `json:"confirm"`
		}
		if err := readJSON(r, &q); err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.eval == nil || st.removal == nil || q.Token != st.removal.Token || q.Confirm != "REMOVE" {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先检查选中游戏，再确认移除"})
			return
		}
		p := st.removal
		st.removal = nil // Single-use confirmation; retries require a new preview.
		eng := Engine{Eval: st.eval, Root: st.root, Home: st.home}
		ids := make([]int, 0, len(p.Items))
		for _, item := range p.Items {
			if item.Eligible {
				ids = append(ids, item.AppID)
			}
		}
		results, err := eng.RemoveSelected(r.Context(), p, st.manifest, storeFetcherFor(r.Context(), ids))
		st.preview = nil
		jsonResponse(w, map[string]any{"ok": err == nil, "error": errText(err), "results": results})
	})
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]any{"ok": true, "error": "工具已退出"})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()
	})
	mux.HandleFunc("/api/restart-steam", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		st.disconnect()
		if e := restart(st.root, true, st.port); e != nil {
			st.startupError = e.Error()
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		e := st.reconnect()
		jsonResponse(w, map[string]any{"ok": e == nil, "error": errText(e), "apps": len(st.snapshot.Apps)})
	})
	mux.HandleFunc("/api/connect", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		e := st.reconnect()

		jsonResponse(w, map[string]any{"ok": e == nil, "error": errText(e), "apps": len(st.snapshot.Apps)})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		var incoming Manifest
		if e := readJSON(r, &incoming); e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		if e := validateManifest(incoming); e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		st.mu.Lock()
		st.manifest = incoming
		st.configSource = "GUI 编辑配置"
		st.preview = nil
		st.mu.Unlock()
		jsonResponse(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/preview", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.eval == nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先连接 Steam"})
			return
		}
		eng := Engine{Eval: st.eval, Root: st.root, Home: st.home}
		snap, err := eng.Snapshot()
		if err != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		st.snapshot = snap
		p := buildPreview(st.manifest, st.snapshot, false)
		st.preview = &p
		jsonResponse(w, map[string]any{"ok": len(p.Blocked) == 0, "error": strings.Join(p.Blocked, "；"), "preview": p})
	})
	mux.HandleFunc("/api/apply", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.eval == nil || st.preview == nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先连接并预览"})
			return
		}
		var req struct {
			Confirm string `json:"confirm"`
		}
		readJSON(r, &req)
		if req.Confirm != "APPLY" {
			jsonResponse(w, map[string]any{"ok": false, "error": "需要输入 APPLY"})
			return
		}
		eng := Engine{Eval: st.eval, Root: st.root, Home: st.home, Overlays: st.manifest.Overlays}
		dir, e := eng.Apply(st.manifest, false, func(p Preview) bool {
			return st.preview.Fingerprint == p.Fingerprint && st.preview.ManifestHash == p.ManifestHash && equalTargets(st.preview.Targets, p.Targets)
		})
		if e == nil {
			st.snapshot, e = eng.Snapshot()
			st.preview = nil
		}
		jsonResponse(w, map[string]any{"ok": e == nil, "error": errText(e), "backup": dir})
	})
	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.eval == nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先连接 Steam"})
			return
		}
		eng := Engine{Eval: st.eval, Root: st.root, Home: st.home, Overlays: st.manifest.Overlays}
		dir, e := eng.ExportLibrary(st.manifest)
		jsonResponse(w, map[string]any{"ok": e == nil, "error": errText(e), "dir": dir})
	})
	mux.HandleFunc("/api/prices/refresh", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			AppIDs []int `json:"appids"`
		}
		if e := readJSON(r, &q); e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		if len(q.AppIDs) == 0 {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先选择一个收藏或筛选游戏"})
			return
		}
		if len(q.AppIDs) > 500 {
			q.AppIDs = q.AppIDs[:500]
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		st.mu.Lock()
		cached := st.prices
		st.mu.Unlock()
		prices := refreshPrices(ctx, q.AppIDs, cached, pricePath(st.home))
		st.mu.Lock()
		st.prices = prices
		enrichManifest(&st.manifest, prices)
		st.mu.Unlock()
		jsonResponse(w, map[string]any{"ok": true, "count": len(q.AppIDs), "prices": prices})
	})
	mux.HandleFunc("/api/ai/models", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			URL string `json:"url"`
			Key string `json:"key"`
		}
		if e := readJSON(r, &q); e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		base := normalizeAIBase(q.URL)
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
		if e == nil && q.Key != "" {
			req.Header.Set("Authorization", "Bearer "+q.Key)
		}
		if e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if resp.StatusCode/100 != 2 {
			jsonResponse(w, map[string]any{"ok": false, "error": fmt.Sprintf("模型接口 HTTP %d: %s", resp.StatusCode, string(b))})
			return
		}
		var data any
		if json.Unmarshal(b, &data) != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "模型接口返回不是 JSON"})
			return
		}
		st.mu.Lock()
		st.aiURL, st.aiKey = q.URL, q.Key
		st.mu.Unlock()
		jsonResponse(w, map[string]any{"ok": true, "data": data})
	})
	mux.HandleFunc("/api/ai/suggest", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			URL     string           `json:"url"`
			Key     string           `json:"key"`
			Model   string           `json:"model"`
			Games   []map[string]any `json:"games"`
			Targets []string         `json:"targets"`
			Prompt  string           `json:"prompt"`
		}
		if e := readJSON(r, &q); e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		if strings.TrimSpace(q.Prompt) == "" {
			jsonResponse(w, map[string]any{"ok": false, "error": "请先填写自己的 AI 提示词"})
			return
		}
		userContent, _ := json.Marshal(map[string]any{"task": q.Prompt, "targets": q.Targets, "games": q.Games})
		body, _ := json.Marshal(map[string]any{"model": q.Model, "temperature": 0.1, "messages": []any{map[string]string{"role": "system", "content": "你是 Steam 收藏规划助手。严格遵守用户提示词。只返回 JSON 对象 {\"suggestions\":[{\"appid\":123,\"target\":\"已有收藏名\",\"reason\":\"原因\"}]}。targets 是现有静态收藏参考；可以按用户要求创建新的静态收藏名称，不要使用动态收藏名称。游戏中的保护、价格、精品等标记是参考资料，只有 target 和 reason 可改变；不得删除许可证或移除保护标记。"}, map[string]any{"role": "user", "content": string(userContent)}}})
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, normalizeAIBase(q.URL)+"/chat/completions", bytes.NewReader(body))
		if e == nil {
			req.Header.Set("Content-Type", "application/json")
			if q.Key != "" {
				req.Header.Set("Authorization", "Bearer "+q.Key)
			}
		}
		if e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": e.Error()})
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if resp.StatusCode/100 != 2 {
			jsonResponse(w, map[string]any{"ok": false, "error": fmt.Sprintf("AI 接口 HTTP %d: %s", resp.StatusCode, string(b))})
			return
		}
		var data any
		if json.Unmarshal(b, &data) != nil {
			jsonResponse(w, map[string]any{"ok": false, "error": "AI 返回不是 JSON"})
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "response": data})
	})
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	portNum := ln.Addr().(*net.TCPAddr).Port
	server = &http.Server{Handler: guiGuard(mux), ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ln) }()
	url := fmt.Sprintf("http://127.0.0.1:%d/", portNum)
	fmt.Println("GUI:", url)
	if e = openGUI(url); e != nil {
		fmt.Println("请在浏览器打开", url)
	}
	go func() {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.eval != nil {
			return
		}
		_ = st.reconnect()
	}()
	serveError := <-serveErr
	if serveError != nil && serveError != http.ErrServerClosed {
		return serveError
	}
	return nil
}

func (st *guiState) readSteam(c Evaluator) error {
	eng := Engine{Eval: c, Root: st.root, Home: st.home}
	s, err := eng.Snapshot()
	if err != nil {
		return err
	}
	m := snapshotManifest(s)
	enrichManifest(&m, st.prices)
	st.eval, st.snapshot, st.manifest = c, s, m
	st.configSource = "Steam 当前收藏"
	st.startupError = ""
	st.startupDone = true
	st.preview = nil
	return nil
}

func (st *guiState) disconnect() {
	if st.eval != nil {
		st.eval.Close()
	}
	st.eval = nil
	st.preview = nil
	st.removal = nil
}

func (st *guiState) reconnect() error {
	st.disconnect()
	c, err := connect(st.port)
	if err == nil {
		err = st.readSteam(c)
		if err != nil {
			c.Close()
		}
	}
	st.startupDone = true
	st.startupError = errText(err)
	return err
}

func errText(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func guiGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			http.Error(w, "Invalid local host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			read := r.URL.Path == "/api/state" || r.URL.Path == "/api/history"
			if read && r.Method != http.MethodGet || !read && r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
