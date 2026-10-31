package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func loadConfiguredManifest(path string) (Manifest, string, error) {
	if path == "" {
		return Manifest{Schema: 2, Games: []Game{}}, "Steam 当前收藏", nil
	}
	if path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return Manifest{}, path, e
		}
		var m Manifest
		if e = json.Unmarshal(b, &m); e != nil {
			return m, path, e
		}
		if e = validateManifest(m); e != nil {
			return m, path, e
		}
		return m, path, nil
	}
	panic("unreachable")
}
func exportDir(home, kind string) string {
	return filepath.Join(home, "exports", kind+"-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
}
func (e *Engine) ExportLibrary(m Manifest) (string, error) {
	saved := e.Overlays
	e.Overlays = nil
	defer func() { e.Overlays = saved }()
	s, err := e.Snapshot()
	if err != nil {
		return "", err
	}
	dir := exportDir(e.Home, "library")
	for _, item := range []struct {
		Name string
		Data any
	}{{"library-export.json", map[string]any{"schema": 2, "kind": "steam-library-export", "exported_at": time.Now().Format(time.RFC3339), "snapshot": s, "membership_complete": true, "availability_verified": false}}, {"games.json", map[string]any{"schema": 1, "account": s.Account, "games": s.Apps}}, {"collections.json", map[string]any{"schema": 1, "account": s.Account, "collections": s.Collections}}} {
		if err = writeJSON(filepath.Join(dir, item.Name), item.Data); err != nil {
			return dir, err
		}
	}
	known := map[int]Game{}
	for _, g := range m.Games {
		known[g.AppID] = g
	}
	edited := m
	edited.Games = []Game{}
	for _, a := range s.Apps {
		g, ok := known[a.AppID]
		if !ok {
			g = Game{AppID: a.AppID, Name: a.Name, Target: "待整理｜新增游戏", Priority: 0, Protect: true, Reason: "新增游戏缺少商店及许可证证据；默认保护，等待手动分类"}
		}
		g.Name = a.Name
		edited.Games = append(edited.Games, g)
	}
	edited.GameCount = len(edited.Games)
	if err = writeJSON(filepath.Join(dir, "editable-classification.json"), edited); err != nil {
		return dir, err
	}
	if err = backupRaw(e, dir); err != nil {
		return dir, err
	}
	return dir, nil
}

type RemovalItem struct {
	AppID      int      `json:"appid"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Reasons    []string `json:"reasons"`
	LastPlayed int64    `json:"last_played"`
}

func (e *Engine) ExportRemovalReview(m Manifest) (string, error) {
	saved := e.Overlays
	e.Overlays = nil
	defer func() { e.Overlays = saved }()
	s, err := e.Snapshot()
	if err != nil {
		return "", err
	}
	apps := map[int]App{}
	for _, a := range s.Apps {
		apps[a.AppID] = a
	}
	items := []RemovalItem{}
	for _, g := range m.Games {
		a, ok := apps[g.AppID]
		if !ok {
			continue
		}
		if !g.FreeCandidate && !g.Protect {
			continue
		}
		status := "review_required_no_removal_permission"
		reasons := []string{"当前免费、未玩且可重新获取只描述现状；未来短期是否转付费或下架不可由此清单保证",
			"移除前必须确认许可证来源、是否为限时免费领取、以及许可证中是否包含其他应用", "尚未验证许可证来源和包内其他游戏；不得按 AppID 直接移除许可证"}
		if g.Protect {
			status = "protected_do_not_remove"
			reasons = append(g.ProtectionReasons, reasons...)
		}
		if a.LastPlayed > 0 {
			status = "protected_do_not_remove"
			reasons = append([]string{"有最新游玩记录"}, reasons...)
		}
		items = append(items, RemovalItem{g.AppID, a.Name, status, reasons, a.LastPlayed})
	}
	dir := exportDir(e.Home, "removal-review")
	err = writeJSON(filepath.Join(dir, "removal-review.json"), map[string]any{"schema": 1, "account": s.Account, "research_date": m.ResearchDate, "exported_at": time.Now().Format(time.RFC3339), "execution_supported": false, "future_short_term_status_guaranteed": false, "items": items})
	return dir, err
}
func ExportCache(root, account, home string, m Manifest) (string, error) {
	if _, err := strconv.ParseUint(account, 10, 32); err != nil || account == "0" {
		return "", fmt.Errorf("请输入有效的 Steam userdata 数字账号目录 ID")
	}
	source := filepath.Join(root, "userdata", account, "config", "cloudstorage", "cloud-storage-namespace-1.json")
	b, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	var pairs [][]json.RawMessage
	if err = json.Unmarshal(b, &pairs); err != nil {
		return "", err
	}
	cols := []rawCollection{}
	ids := map[int]bool{}
	bad := 0
	for _, p := range pairs {
		if len(p) != 2 {
			bad++
			continue
		}
		var key string
		var v struct {
			Value   string `json:"value"`
			Deleted bool   `json:"is_deleted"`
		}
		if json.Unmarshal(p[0], &key) != nil || !strings.HasPrefix(key, "user-collections.") {
			continue
		}
		if json.Unmarshal(p[1], &v) != nil {
			bad++
			continue
		}
		if v.Deleted {
			continue
		}
		var c rawCollection
		if json.Unmarshal([]byte(v.Value), &c) != nil {
			bad++
			continue
		}
		if c.ID == "" {
			c.ID = strings.TrimPrefix(key, "user-collections.")
		}
		cols = append(cols, c)
		for _, id := range append(append([]int{}, c.Added...), c.Removed...) {
			if id > 0 {
				ids[id] = true
			}
		}
	}
	names := map[int]string{}
	for _, g := range m.Games {
		names[g.AppID] = g.Name
	}
	apps := []App{}
	ordered := []int{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	for _, id := range normIDs(ordered) {
		name := names[id]
		if name == "" {
			name = fmt.Sprint("App ", id)
		}
		apps = append(apps, App{AppID: id, Name: name})
	}
	dir := exportDir(home, "cache")
	if err = writeJSON(filepath.Join(dir, "cache-export.json"), map[string]any{"schema": 1, "kind": "steam-cache-export", "account": account, "source": source, "exported_at": time.Now().Format(time.RFC3339), "membership_complete": false, "ownership_proven": false, "collections": cols, "appids_seen_in_cache": apps, "malformed_entries": bad, "notes": []string{"动态收藏仅能读取规则和明确加入/排除项，不能在离线缓存中重算全体成员", "AppID 出现在排除列表不等于当前拥有该游戏", "完整已拥有游戏列表请使用在线导出功能"}}); err != nil {
		return dir, err
	}
	e := Engine{Root: root, Home: home, Account: account}
	if err = backupRaw(&e, dir); err != nil {
		return dir, err
	}
	return dir, nil
}
