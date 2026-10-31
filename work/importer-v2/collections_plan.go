package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func snapshotManifest(s Snapshot) Manifest {
	m := Manifest{Schema: 2, CollectionState: &s, Games: []Game{}}
	members := map[int][]string{}
	for _, c := range s.Collections {
		if !c.Dynamic && (!c.System || c.ID == "favorite") {
			m.CollectionOrder = append(m.CollectionOrder, c.Name)
			for _, id := range c.AppIDs {
				members[id] = append(members[id], c.Name)
			}
		}
	}
	for _, app := range s.Apps {
		target := "未分类"
		if len(members[app.AppID]) > 0 {
			target = members[app.AppID][0]
		}
		m.Games = append(m.Games, Game{AppID: app.AppID, Name: app.Name, Target: target, Priority: 6})
	}
	m.GameCount = len(m.Games)
	return m
}

func enrichManifest(m *Manifest, prices map[int]PriceInfo) {
	for i := range m.Games {
		if p, ok := prices[m.Games[i].AppID]; ok {
			if len(p.Publishers) > 0 {
				m.Games[i].Publishers = append([]string{}, p.Publishers...)
			}
			if len(p.Developers) > 0 {
				m.Games[i].Developers = append([]string{}, p.Developers...)
			}
		}
	}
}

// Accept classification rules, full library exports, snapshots, and the plain
// collections.json format. Dynamic recipes are retained as read-only metadata.
func normalizeImport(raw json.RawMessage, current Snapshot) (Manifest, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Manifest{}, err
	}
	if b, ok := probe["membership_complete"]; ok && string(b) == "false" {
		return Manifest{}, fmt.Errorf("离线缓存缺少完整成员信息，请先读取 Steam 并导出收藏快照后导入")
	}
	if _, ok := probe["games"]; ok {
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return m, err
		}
		if m.GameCount > 0 {
			return m, validateManifest(m)
		}
		if len(m.Games) > 0 {
			m.Schema = 2
			m.GameCount = len(m.Games)
			for i := range m.Games {
				if m.Games[i].Target == "" {
					m.Games[i].Target = "待整理｜导入游戏"
					m.Games[i].Priority = 6
					m.Games[i].Protect = true
				}
			}
			return m, validateManifest(m)
		}
	}
	var s Snapshot
	if b, ok := probe["snapshot"]; ok {
		if err := json.Unmarshal(b, &s); err != nil {
			return Manifest{}, err
		}
	} else if _, ok := probe["collections"]; ok {
		if err := json.Unmarshal(raw, &s); err != nil {
			return Manifest{}, err
		}
	} else {
		return Manifest{}, fmt.Errorf("支持分类规则、Steam 库导出和收藏快照 JSON")
	}
	// The human-readable grouped deliverable also imports as a rule manifest.
	var grouped struct {
		Collections []struct {
			Name  string `json:"name"`
			Games []Game `json:"games"`
		} `json:"collections"`
	}
	_ = json.Unmarshal(raw, &grouped)
	m := Manifest{Schema: 2, Games: []Game{}}
	for _, c := range grouped.Collections {
		for _, g := range c.Games {
			if g.Target == "" {
				g.Target = c.Name
			}
			m.Games = append(m.Games, g)
		}
	}
	if len(m.Games) > 0 {
		m.GameCount = len(m.Games)
		return m, validateManifest(m)
	}
	if len(s.Collections) == 0 {
		return Manifest{}, fmt.Errorf("收藏快照为空")
	}
	for i, c := range s.Collections {
		if len(c.Filter) > 0 && string(c.Filter) != "null" && string(c.Filter) != "{}" {
			s.Collections[i].Dynamic = true
		}
	}
	if len(s.Apps) == 0 {
		s.Apps = append([]App{}, current.Apps...)
	}
	if len(s.Apps) == 0 {
		seen := map[int]bool{}
		for _, c := range s.Collections {
			for _, id := range c.AppIDs {
				if !seen[id] {
					s.Apps = append(s.Apps, App{AppID: id, Name: fmt.Sprintf("App %d", id)})
					seen[id] = true
				}
			}
		}
	}
	m = snapshotManifest(s)
	return m, validateManifest(m)
}

func buildCollectionPreview(m Manifest, s Snapshot) Preview {
	p := Preview{Schema: 2, CollectionState: m.CollectionState, Account: s.Account, ManifestHash: manifestDigest(m), ManifestCount: m.GameCount, LibraryCount: len(s.Apps), Targets: map[int]string{}, Actions: []Action{}, Missing: []Game{}, Blocked: []string{}, Fingerprint: fingerprint(s)}
	if s.Account == "" || s.Account == "0" {
		p.Blocked = append(p.Blocked, "未确认 Steam 当前账号")
	}
	if m.CollectionState.Account != "" && m.CollectionState.Account != s.Account {
		p.Blocked = append(p.Blocked, "收藏快照属于其他账号，不能保存到当前账号")
	}
	owned := map[int]bool{}
	for _, a := range s.Apps {
		owned[a.AppID] = true
	}
	for _, g := range m.Games {
		if owned[g.AppID] {
			p.Targets[g.AppID] = g.Target
			p.FoundTotal++
		} else {
			p.Missing = append(p.Missing, g)
		}
	}
	p.ScopeCount = len(p.Targets)
	if p.FoundTotal*100 < m.GameCount*80 {
		p.Blocked = append(p.Blocked, "快照游戏与当前库匹配不足 80%，请核对账号和库范围")
	}
	desired := map[string]Collection{}
	for _, c := range m.CollectionState.Collections {
		if c.Dynamic || c.System && c.ID != "favorite" {
			continue
		}
		ids := []int{}
		for _, id := range c.AppIDs {
			if owned[id] {
				ids = append(ids, id)
			}
		}
		c.AppIDs = normIDs(ids)
		desired[c.Name] = c
	}
	live := append([]Collection{}, s.Collections...)
	names := []string{}
	for n := range desired {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		want := desired[n]
		idx := -1
		for i, c := range live {
			if (c.Name == n && (!c.System || c.ID == "favorite")) || want.ID == "favorite" && c.ID == "favorite" {
				if idx >= 0 {
					p.Blocked = append(p.Blocked, "收藏名称重复："+n)
				}
				idx = i
			} else if !c.System && strings.EqualFold(c.Name, n) {
				p.Blocked = append(p.Blocked, "收藏名称大小写冲突："+n)
			}
		}
		if idx < 0 {
			if len(want.AppIDs) == 0 {
				continue
			}
			id := ""
			if want.ID == "favorite" {
				id = "favorite"
			}
			a := Action{Phase: "ensure_target", ID: id, Name: n, Before: []int{}, After: want.AppIDs}
			p.Actions = append(p.Actions, a)
			live = append(live, Collection{ID: "@new:" + n, Name: n, Editable: true, AppIDs: want.AppIDs})
			continue
		}
		c := live[idx]
		if c.Dynamic {
			p.Blocked = append(p.Blocked, "静态快照目标与当前动态收藏重名："+n)
			continue
		}
		after := union(c.AppIDs, want.AppIDs)
		if !sameIDs(c.AppIDs, after) {
			if !c.Editable {
				p.Blocked = append(p.Blocked, "收藏不可编辑："+n)
			}
			p.Actions = append(p.Actions, Action{Phase: "ensure_target", ID: c.ID, Name: c.Name, Before: c.AppIDs, After: after})
			live[idx].AppIDs = after
		}
	}
	for _, c := range live {
		if c.Dynamic || c.System && c.ID != "favorite" {
			continue
		}
		want, exists := desired[c.Name]
		if c.ID == "favorite" {
			for _, d := range desired {
				if d.ID == "favorite" {
					want = d
					exists = true
				}
			}
		}
		after := []int{}
		wanted := map[int]bool{}
		if exists {
			for _, id := range want.AppIDs {
				wanted[id] = true
			}
		}
		for _, id := range c.AppIDs {
			_, inScope := p.Targets[id]
			if !inScope || wanted[id] {
				after = append(after, id)
			}
		}
		if !exists && len(after) == 0 && !c.System {
			if !c.Deletable {
				p.Blocked = append(p.Blocked, "收藏不可删除："+c.Name)
				continue
			}
			p.Actions = append(p.Actions, Action{Phase: "delete_collection", ID: c.ID, Name: c.Name, Before: c.AppIDs, After: []int{}})
			continue
		}
		if !sameIDs(c.AppIDs, after) {
			if !c.Editable {
				p.Blocked = append(p.Blocked, "收藏不可编辑："+c.Name)
			}
			p.Actions = append(p.Actions, Action{Phase: "reconcile", ID: c.ID, Name: c.Name, Before: c.AppIDs, After: normIDs(after)})
		}
	}
	return p
}

func auditPreview(s Snapshot, p Preview, before *Snapshot) Audit {
	if p.CollectionState == nil {
		return audit(s, p.Targets, before, p.OverlaySpecs)
	}
	a := audit(s, map[int]string{}, before)
	a.Scope = len(p.Targets)
	a.Problems = map[int][]string{}
	a.Verified = []int{}
	desired := map[int][]string{}
	actual := map[int][]string{}
	for _, c := range p.CollectionState.Collections {
		if c.Dynamic || c.System && c.ID != "favorite" {
			continue
		}
		for _, id := range c.AppIDs {
			desired[id] = append(desired[id], c.Name)
		}
	}
	for _, c := range s.Collections {
		if c.Dynamic || c.System && c.ID != "favorite" {
			continue
		}
		for _, id := range c.AppIDs {
			actual[id] = append(actual[id], c.Name)
		}
	}
	for id := range p.Targets {
		sort.Strings(desired[id])
		sort.Strings(actual[id])
		if strings.Join(desired[id], "\x00") == strings.Join(actual[id], "\x00") {
			a.Verified = append(a.Verified, id)
		} else {
			a.Problems[id] = append([]string{"expected: " + strings.Join(desired[id], ", ")}, actual[id]...)
		}
	}
	// Reuse normal audit's outside-scope and dynamic immutability checks.
	base := audit(s, p.Targets, before)
	a.OutsideUnchanged = base.OutsideUnchanged
	a.OverlayProblems = base.OverlayProblems
	return a
}
