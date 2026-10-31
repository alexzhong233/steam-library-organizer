package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Game struct {
	AppID    int    `json:"appid"`
	Name     string `json:"name"`
	Target   string `json:"target"`
	Priority int    `json:"priority"`
}
type Manifest struct {
	Schema       int    `json:"schema"`
	ResearchDate string `json:"research_date"`
	GameCount    int    `json:"game_count"`
	Games        []Game `json:"games"`
}
type App struct {
	AppID      int    `json:"appid"`
	Name       string `json:"name"`
	LastPlayed int64  `json:"last_played"`
}
type Collection struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Dynamic         bool            `json:"dynamic"`
	System          bool            `json:"system"`
	Editable        bool            `json:"editable"`
	Deletable       bool            `json:"deletable"`
	Filter          json.RawMessage `json:"filter"`
	AppIDs          []int           `json:"appids"`
	ExplicitAdded   []int           `json:"explicit_added,omitempty"`
	ExplicitRemoved []int           `json:"explicit_removed,omitempty"`
	FilterCaptured  bool            `json:"filter_captured"`
}
type Snapshot struct {
	Schema      int          `json:"schema"`
	Account     string       `json:"account"`
	Apps        []App        `json:"apps"`
	Collections []Collection `json:"collections"`
}
type Action struct {
	Phase           string          `json:"phase"`
	ID              string          `json:"id,omitempty"`
	Name            string          `json:"name"`
	Before          []int           `json:"before"`
	After           []int           `json:"after"`
	Freeze          bool            `json:"freeze_dynamic"`
	RestoreDynamic  bool            `json:"restore_dynamic,omitempty"`
	Filter          json.RawMessage `json:"filter,omitempty"`
	ExplicitAdded   []int           `json:"explicit_added,omitempty"`
	ExplicitRemoved []int           `json:"explicit_removed,omitempty"`
}
type Preview struct {
	Schema        int            `json:"schema"`
	Account       string         `json:"account"`
	ManifestHash  string         `json:"manifest_sha256"`
	Pilot         bool           `json:"pilot"`
	ManifestCount int            `json:"manifest_count"`
	LibraryCount  int            `json:"library_count"`
	FoundTotal    int            `json:"found_total"`
	ScopeCount    int            `json:"scope_count"`
	Missing       []Game         `json:"missing"`
	Promoted      []int          `json:"played_promoted_appids"`
	Unresolved    int            `json:"unresolved_availability"`
	Targets       map[int]string `json:"effective_targets"`
	Actions       []Action       `json:"actions"`
	Blocked       []string       `json:"blocked"`
	Fingerprint   string         `json:"snapshot_fingerprint"`
}

func manifest() (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return m, err
	}
	seen := map[int]bool{}
	for _, g := range m.Games {
		if g.AppID <= 0 || seen[g.AppID] || strings.TrimSpace(g.Target) == "" {
			return m, fmt.Errorf("分类清单无效或 AppID 重复")
		}
		seen[g.AppID] = true
	}
	if m.Schema != 1 || len(seen) != m.GameCount || m.GameCount != len(m.Games) {
		return m, fmt.Errorf("分类清单数量/格式不符")
	}
	return m, nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func normIDs(xs []int) []int {
	m := map[int]bool{}
	for _, x := range xs {
		m[x] = true
	}
	out := []int{}
	for x := range m {
		out = append(out, x)
	}
	sort.Ints(out)
	return out
}
func sameIDs(a, b []int) bool {
	x, y := normIDs(a), normIDs(b)
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		if y[i] != v {
			return false
		}
	}
	return true
}
func union(a, b []int) []int { return normIDs(append(append([]int{}, a...), b...)) }
func sameFilter(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}
func usableFilter(f json.RawMessage) bool {
	var x map[string]any
	return json.Unmarshal(f, &x) == nil && x != nil && x["filterGroups"] != nil
}
func fingerprint(s Snapshot) string {
	// Ignore playtime timestamps but bind preview to account, library identities,
	// filter rules, names, and memberships. Time played is refreshed on re-preview.
	ids := []int{}
	for _, a := range s.Apps {
		ids = append(ids, a.AppID)
	}
	cs := append([]Collection{}, s.Collections...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
	for i := range cs {
		cs[i].AppIDs = normIDs(cs[i].AppIDs)
	}
	b, _ := json.Marshal(struct {
		Account     string
		Apps        []int
		Collections []Collection
	}{s.Account, normIDs(ids), cs})
	return digest(b)
}
func buildPreview(m Manifest, s Snapshot, pilot bool) Preview {
	p := Preview{Schema: 1, Account: s.Account, ManifestHash: digest(manifestBytes), Pilot: pilot, ManifestCount: m.GameCount, Targets: map[int]string{}, Actions: []Action{}, Missing: []Game{}, Promoted: []int{}, Blocked: []string{}, Fingerprint: fingerprint(s)}
	library := map[int]App{}
	for _, a := range s.Apps {
		library[a.AppID] = a
	}
	p.LibraryCount = len(library)
	for _, g := range m.Games {
		if _, ok := library[g.AppID]; ok {
			p.FoundTotal++
		}
	}
	if p.FoundTotal*100 < m.GameCount*80 {
		p.Blocked = append(p.Blocked, "库中匹配不足清单的 80%；可能是错误账号、库未载入或导出范围不同。未启用写入。")
	}
	if s.Account == "" || s.Account == "0" {
		p.Blocked = append(p.Blocked, "未能确认 Windows Steam 当前账号，未启用写入。")
	}
	games := m.Games
	if pilot {
		games = []Game{}
		for _, g := range m.Games {
			if g.Target == "系列｜Call of Duty（使命召唤）" {
				games = append(games, g)
			}
		}
		sort.Slice(games, func(i, j int) bool { return games[i].AppID < games[j].AppID })
		if len(games) > 5 {
			games = games[:5]
		}
	}
	for _, g := range games {
		a, ok := library[g.AppID]
		if !ok {
			p.Missing = append(p.Missing, g)
			continue
		}
		t := g.Target
		if g.Priority == 4 && a.LastPlayed > 0 {
			t = "免费绝版/游玩游戏"
			p.Promoted = append(p.Promoted, g.AppID)
		}
		p.Targets[g.AppID] = t
		if g.Priority == 0 {
			p.Unresolved++
		}
	}
	p.ScopeCount = len(p.Targets)
	want := map[string][]int{}
	for id, t := range p.Targets {
		want[t] = append(want[t], id)
	}
	names := []string{}
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	// Simulate additive target creation first, so no game loses its old home
	// before its target exists. Placeholder ids are resolved by exact name later.
	simulated := append([]Collection{}, s.Collections...)
	for _, name := range names {
		for _, c := range simulated {
			if !c.System && c.Name != name && strings.EqualFold(c.Name, name) {
				p.Blocked = append(p.Blocked, "目标名称与已有收藏仅大小写不同："+name)
			}
		}
		idx := -1
		for i, c := range simulated {
			if !c.System && c.Name == name {
				if idx >= 0 {
					p.Blocked = append(p.Blocked, "目标收藏重名："+name)
				}
				idx = i
			}
		}
		if idx < 0 {
			a := Action{Phase: "ensure_target", Name: name, Before: []int{}, After: normIDs(want[name])}
			p.Actions = append(p.Actions, a)
			simulated = append(simulated, Collection{ID: "@new:" + name, Name: name, Editable: true, Deletable: true, AppIDs: a.After})
			continue
		}
		c := simulated[idx]
		after := union(c.AppIDs, want[name])
		if !sameIDs(c.AppIDs, after) || c.Dynamic {
			p.Actions = append(p.Actions, Action{Phase: "ensure_target", ID: c.ID, Name: name, Before: normIDs(c.AppIDs), After: after, Freeze: c.Dynamic, Filter: c.Filter, ExplicitAdded: c.ExplicitAdded, ExplicitRemoved: c.ExplicitRemoved})
			if c.Dynamic {
				if !c.Deletable || !c.FilterCaptured {
					p.Blocked = append(p.Blocked, "动态收藏不能安全备份/转换："+c.Name)
				}
				c.Dynamic = false
				c.ID = "@new:" + name
				c.Editable = true
				c.Filter = nil
			} else if !c.Editable {
				p.Blocked = append(p.Blocked, "目标收藏不可编辑："+name)
			}
			c.AppIDs = after
			simulated[idx] = c
		}
	}
	for _, c := range simulated {
		if c.System && c.ID != "favorite" {
			continue
		}
		after := []int{}
		for _, id := range c.AppIDs {
			target, in := p.Targets[id]
			if !in || (!c.System && target == c.Name) {
				after = append(after, id)
			}
		}
		after = normIDs(after)
		if sameIDs(after, c.AppIDs) {
			continue
		}
		if c.Dynamic {
			if !c.Deletable || !c.FilterCaptured {
				p.Blocked = append(p.Blocked, "动态收藏不能安全备份/转换："+c.Name)
			}
		} else if !c.Editable {
			p.Blocked = append(p.Blocked, "旧收藏不可移出成员："+c.Name)
		}
		p.Actions = append(p.Actions, Action{Phase: "reconcile", ID: c.ID, Name: c.Name, Before: normIDs(c.AppIDs), After: after, Freeze: c.Dynamic, Filter: c.Filter, ExplicitAdded: c.ExplicitAdded, ExplicitRemoved: c.ExplicitRemoved})
	}
	touched := map[string]bool{}
	for _, act := range p.Actions {
		touched[act.Name] = true
	}
	counts := map[string]int{}
	for _, c := range s.Collections {
		if !c.System {
			counts[c.Name]++
		}
	}
	for name, n := range counts {
		if n > 1 && touched[name] {
			p.Blocked = append(p.Blocked, "受影响的收藏存在重名，无法无歧义恢复："+name)
		}
	}
	return p
}

type Audit struct {
	Scope            int              `json:"scope"`
	Verified         []int            `json:"verified"`
	Problems         map[int][]string `json:"problems"`
	OutsideUnchanged bool             `json:"outside_unchanged"`
}

func audit(s Snapshot, targets map[int]string, before *Snapshot) Audit {
	a := Audit{Scope: len(targets), Verified: []int{}, Problems: map[int][]string{}, OutsideUnchanged: true}
	found := map[int][]string{}
	for _, c := range s.Collections {
		if c.System && c.ID != "favorite" {
			continue
		}
		for _, id := range c.AppIDs {
			if _, ok := targets[id]; ok {
				found[id] = append(found[id], c.Name)
			}
		}
	}
	for id, w := range targets {
		cs := found[id]
		if len(cs) == 1 && cs[0] == w {
			a.Verified = append(a.Verified, id)
		} else {
			a.Problems[id] = append([]string{"expected: " + w}, cs...)
		}
	}
	if before != nil {
		// Compare all memberships outside scope by collection NAME, allowing only
		// the ID change required by an explicitly recorded dynamic conversion.
		signature := func(s Snapshot) string {
			rows := []string{}
			for _, c := range s.Collections {
				for _, id := range c.AppIDs {
					if _, in := targets[id]; !in || c.System && c.ID != "favorite" {
						rows = append(rows, fmt.Sprintf("%s\x00%s\x00%d", c.Name, fmt.Sprint(c.System), id))
					}
				}
			}
			sort.Strings(rows)
			return strings.Join(rows, "\n")
		}
		a.OutsideUnchanged = signature(*before) == signature(s)
	}
	sort.Ints(a.Verified)
	return a
}
