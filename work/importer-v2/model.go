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
	Publishers             []string `json:"publishers,omitempty"`
	Developers             []string `json:"developers,omitempty"`
	AppID                  int      `json:"appid"`
	Name                   string   `json:"name"`
	Target                 string   `json:"target"`
	Priority               int      `json:"priority"`
	KeepPrimary            bool     `json:"keep_primary"`
	Protect                bool     `json:"protect"`
	ProtectionReasons      []string `json:"protection_reasons,omitempty"`
	QualityIndie           bool     `json:"quality_indie"`
	LowValue               bool     `json:"low_value"`
	FreeCandidate          bool     `json:"free_candidate"`
	FutureAvailabilityRisk string   `json:"future_availability_risk,omitempty"`
	PlayedTarget           string   `json:"played_target,omitempty"`
	DynamicOnlyIfMatched   bool     `json:"dynamic_only_if_matched"`
	Reason                 string   `json:"reason,omitempty"`
	Series                 string   `json:"series,omitempty"`
}
type Manifest struct {
	CollectionState *Snapshot `json:"collection_state,omitempty"`
	Schema          int       `json:"schema"`
	ResearchDate    string    `json:"research_date"`
	GameCount       int       `json:"game_count"`
	Games           []Game    `json:"games"`
	Overlays        []Overlay `json:"overlays,omitempty"`
	CollectionOrder []string  `json:"collection_order,omitempty"`
}
type App struct {
	AppID           int    `json:"appid"`
	Name            string `json:"name"`
	LastPlayed      int64  `json:"last_played"`
	PlaytimeMinutes *int64 `json:"playtime_minutes,omitempty"`
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
	Projections []Projection `json:"overlay_projections,omitempty"`
}
type Action struct {
	Phase           string          `json:"phase"`
	Overlay         bool            `json:"overlay,omitempty"`
	BeforeName      string          `json:"before_name,omitempty"`
	Recovery        bool            `json:"recovery,omitempty"`
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
	CollectionState *Snapshot      `json:"collection_state,omitempty"`
	Schema          int            `json:"schema"`
	OverlaySpecs    []Overlay      `json:"overlay_specs,omitempty"`
	DynamicOnly     []int          `json:"dynamic_only_appids,omitempty"`
	FreeCandidates  []int          `json:"free_candidate_appids,omitempty"`
	Account         string         `json:"account"`
	ManifestHash    string         `json:"manifest_sha256"`
	Pilot           bool           `json:"pilot"`
	ManifestCount   int            `json:"manifest_count"`
	LibraryCount    int            `json:"library_count"`
	FoundTotal      int            `json:"found_total"`
	ScopeCount      int            `json:"scope_count"`
	Missing         []Game         `json:"missing"`
	Promoted        []int          `json:"played_promoted_appids"`
	Unresolved      int            `json:"unresolved_availability"`
	Targets         map[int]string `json:"effective_targets"`
	Actions         []Action       `json:"actions"`
	Blocked         []string       `json:"blocked"`
	Fingerprint     string         `json:"snapshot_fingerprint"`
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
func manifestDigest(m Manifest) string { b, _ := json.Marshal(m); return digest(b) }
func buildPreview(m Manifest, s Snapshot, pilot bool) Preview {
	if m.CollectionState != nil {
		return buildCollectionPreview(m, s)
	}
	p := Preview{Schema: 2, OverlaySpecs: m.Overlays, Account: s.Account, ManifestHash: manifestDigest(m), Pilot: pilot, ManifestCount: m.GameCount, Targets: map[int]string{}, Actions: []Action{}, Missing: []Game{}, Promoted: []int{}, Blocked: []string{}, Fingerprint: fingerprint(s)}
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
	simulated, overlayMembership := prepareOverlays(m, s, &p)
	games := m.Games
	if pilot {
		games = []Game{}
		for _, g := range m.Games {
			if strings.Contains(g.Target, "Call of Duty") {
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
		if t == "未分类" {
			t = ""
		}
		if g.Protect && g.FreeCandidate {
			t = "免费保护｜禁止移除"
		}
		if g.Priority == 4 && a.LastPlayed > 0 {
			if g.PlayedTarget != "" {
				t = g.PlayedTarget
			} else {
				t = "待核实｜玩法类型"
			}
			p.Promoted = append(p.Promoted, g.AppID)
		}
		candidate := g.FreeCandidate && !g.Protect && a.LastPlayed <= 0
		if candidate {
			p.FreeCandidates = append(p.FreeCandidates, g.AppID)
		}
		if !candidate && !g.LowValue && !g.KeepPrimary && !g.Protect && !g.QualityIndie && g.DynamicOnlyIfMatched && len(overlayMembership[g.AppID]) > 0 {
			t = ""
			p.DynamicOnly = append(p.DynamicOnly, g.AppID)
		}
		p.Targets[g.AppID] = t
		if g.Priority == 0 {
			p.Unresolved++
		}
	}
	p.ScopeCount = len(p.Targets)
	want := map[string][]int{}
	for id, t := range p.Targets {
		if t == "" {
			continue
		}
		want[t] = append(want[t], id)
	}
	names := []string{}
	for n := range want {
		names = append(names, n)
	}
	order := map[string]int{}
	for i, n := range m.CollectionOrder {
		order[n] = i
	}
	sort.Slice(names, func(i, j int) bool {
		ai, oki := order[names[i]]
		aj, okj := order[names[j]]
		if oki && okj {
			return ai < aj
		}
		if oki {
			return true
		}
		if okj {
			return false
		}
		return names[i] < names[j]
	})
	// Simulate additive target creation first, so no game loses its old home
	// before its target exists. Placeholder ids are resolved by exact name later.
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
		if c.Dynamic {
			p.Blocked = append(p.Blocked, "目标收藏“"+name+"”是用户动态收藏；请在 Steam 创建同名静态收藏或修改导入目标")
			continue
		}
		after := union(c.AppIDs, want[name])
		if !sameIDs(c.AppIDs, after) {
			p.Actions = append(p.Actions, Action{Phase: "ensure_target", ID: c.ID, Name: name, Before: normIDs(c.AppIDs), After: after, Freeze: c.Dynamic, Filter: c.Filter, ExplicitAdded: c.ExplicitAdded, ExplicitRemoved: c.ExplicitRemoved})
			if !c.Editable {
				p.Blocked = append(p.Blocked, "目标收藏不可编辑："+name)
			}
			c.AppIDs = after
			simulated[idx] = c
		}
	}
	for _, c := range simulated {
		if c.Dynamic {
			continue
		}
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
	OverlayProblems  []string         `json:"overlay_problems"`
}

func audit(s Snapshot, targets map[int]string, before *Snapshot, policies ...[]Overlay) Audit {
	defs := []Overlay{}
	if len(policies) > 0 {
		defs = policies[0]
	}
	a := Audit{Scope: len(targets), Verified: []int{}, Problems: map[int][]string{}, OutsideUnchanged: true, OverlayProblems: []string{}}
	_ = defs // Legacy manifest overlay definitions are accepted for import only.
	allowed := map[string]bool{}
	for _, c := range s.Collections {
		if c.Dynamic && !c.System {
			allowed[c.ID] = true
		}
	}
	ordinary := map[int][]string{}
	over := map[int][]string{}
	for _, c := range s.Collections {
		if c.System && c.ID != "favorite" {
			continue
		}
		for _, id := range c.AppIDs {
			if _, in := targets[id]; !in {
				continue
			}
			if allowed[c.ID] {
				over[id] = append(over[id], c.Name)
			} else {
				ordinary[id] = append(ordinary[id], c.Name)
			}
		}
	}
	for id, w := range targets {
		cs := ordinary[id]
		ok := len(cs) == 1 && cs[0] == w
		if w == "" {
			ok = len(cs) == 0
		}
		if ok {
			a.Verified = append(a.Verified, id)
		} else {
			a.Problems[id] = append([]string{"expected primary: " + w}, cs...)
		}
	}
	if before != nil {
		live := map[string]Collection{}
		for _, c := range s.Collections {
			live[c.ID] = c
		}
		for _, c := range before.Collections {
			if !c.Dynamic {
				continue
			}
			after, ok := live[c.ID]
			if !ok || !after.Dynamic || after.Name != c.Name || !sameFilter(after.Filter, c.Filter) || !sameIDs(after.AppIDs, c.AppIDs) {
				a.OverlayProblems = append(a.OverlayProblems, "用户动态收藏发生变化："+c.Name)
			}
		}
		signature := func(s Snapshot) string {
			rows := []string{}
			for _, c := range s.Collections {
				if c.Dynamic {
					continue
				}
				for _, id := range c.AppIDs {
					if _, in := targets[id]; !in || c.System && c.ID != "favorite" {
						rows = append(rows, fmt.Sprintf("%s\x00%v\x00%d", c.Name, c.System, id))
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
