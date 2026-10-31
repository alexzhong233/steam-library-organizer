package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The personal classification list is a source test fixture, never embedded in the app.
func manifest() (Manifest, error) {
	// 个人清单是测试夹具、不进 Git；公开仓库用 classification.example.json 兜底。
	var b []byte
	var err error
	for _, name := range []string{"classification.json", "classification.example.json"} {
		if b, err = os.ReadFile(name); err == nil {
			break
		}
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, validateManifest(m)
}

// usesPersonalFixture 判断当前夹具是不是本机个人清单。
// 个人清单不进 Git，公开仓库只有 5 条示例；依赖具体 AppID 的断言必须跳过。
func usesPersonalFixture(t *testing.T, m Manifest) {
	t.Helper()
	if m.ResearchDate == "example" || m.GameCount < 100 {
		t.Skip("需要本机 classification.json 个人清单；当前只有示例数据")
	}
}

func testManifest() Manifest {
	return Manifest{Schema: 1, GameCount: 4, Games: []Game{{AppID: 1, Name: "One", Target: "Series", Priority: 1}, {AppID: 2, Name: "Two", Target: "Free", Priority: 4, PlayedTarget: "Free"}, {AppID: 3, Name: "Three", Target: "Delisted", Priority: 2}, {AppID: 4, Name: "Four", Target: "Genre", Priority: 6}}}
}
func testSnapshot() Snapshot {
	return Snapshot{Schema: 1, Account: "123", Apps: []App{{AppID: 1, Name: "One"}, {AppID: 2, Name: "Two", LastPlayed: 12345}, {AppID: 3, Name: "Three"}, {AppID: 4, Name: "Four"}, {AppID: 99, Name: "Extra"}}, Collections: []Collection{
		{ID: "old", Name: "Old", Editable: true, Deletable: true, AppIDs: []int{1, 2, 99}},
		{ID: "favorite", Name: "Favorites", System: true, Editable: true, AppIDs: []int{1, 99}},
		{ID: "hidden", Name: "Hidden", System: true, AppIDs: []int{2, 99}},
	}}
}
func addUserOverlays(m Manifest, s *Snapshot) {
	for i, o := range m.Overlays {
		id := "user-overlay-" + string(rune('a'+i))
		s.Collections = append(s.Collections, Collection{ID: id, Name: o.Name, Dynamic: true, Editable: true, Deletable: true, FilterCaptured: true, Filter: o.Filter})
		s.Projections = append(s.Projections, Projection{Name: o.Name, SourceID: id, Filter: o.Filter, AppIDs: []int{}})
	}
}
func TestManifestAndPilot(t *testing.T) {
	m, e := manifest()
	if e != nil {
		t.Fatal(e)
	}
	s := Snapshot{Schema: 1, Account: "123"}
	for _, g := range m.Games {
		s.Apps = append(s.Apps, App{AppID: g.AppID})
	}
	addUserOverlays(m, &s)
	p := buildPreview(m, s, true)
	if len(p.Targets) != 5 || len(p.Blocked) > 0 {
		t.Fatalf("pilot=%d blocked=%v", len(p.Targets), p.Blocked)
	}
	for _, n := range p.Targets {
		if !strings.Contains(n, "Call of Duty") {
			t.Fatal(n)
		}
	}
}
func TestPriorityAndOutsidePreservation(t *testing.T) {
	s := testSnapshot()
	p := buildPreview(testManifest(), s, false)
	if len(p.Blocked) > 0 || p.Targets[2] != "Free" {
		t.Fatal(p)
	}
	seenCleanup := false
	for _, a := range p.Actions {
		if a.Phase == "reconcile" {
			seenCleanup = true
			if a.ID == "old" && !sameIDs(a.After, []int{99}) {
				t.Fatal(a)
			}
		} else if seenCleanup {
			t.Fatal("targets must be saved before cleanup")
		}
		if a.ID == "hidden" {
			t.Fatal("hidden modified")
		}
	}
}
func TestPreflightBlocks(t *testing.T) {
	t.Run("wrong library", func(t *testing.T) {
		s := testSnapshot()
		s.Apps = s.Apps[:1]
		if len(buildPreview(testManifest(), s, false).Blocked) == 0 {
			t.Fatal("allowed low coverage")
		}
	})
	t.Run("duplicate target", func(t *testing.T) {
		s := testSnapshot()
		s.Collections = append(s.Collections, Collection{ID: "a", Name: "Series"}, Collection{ID: "b", Name: "Series"})
		if len(buildPreview(testManifest(), s, false).Blocked) == 0 {
			t.Fatal("allowed duplicate")
		}
	})
	t.Run("user dynamic remains untouched", func(t *testing.T) {
		s := testSnapshot()
		s.Collections[0].Dynamic = true
		p := buildPreview(testManifest(), s, false)
		if len(p.Blocked) != 0 {
			t.Fatal("unrelated user dynamic collection blocked plan", p.Blocked)
		}
		for _, a := range p.Actions {
			if a.ID == s.Collections[0].ID {
				t.Fatal("user dynamic collection got write action", a)
			}
		}
	})
}
func TestDynamicFreezeCarriesRecipe(t *testing.T) {
	s := testSnapshot()
	c := &s.Collections[0]
	c.Dynamic = true
	c.FilterCaptured = true
	c.Filter = json.RawMessage(`{"filterGroups":[]}`)
	c.ExplicitAdded = []int{99}
	p := buildPreview(testManifest(), s, false)
	if len(p.Blocked) > 0 {
		t.Fatal(p.Blocked)
	}
	found := false
	for _, a := range p.Actions {
		if a.ID == "old" {
			found = true
			t.Fatal("dynamic collection must never be frozen or edited", a)
		}
	}
	if found {
		t.Fatal("dynamic collection received an action")
	}
}
func TestAuditDetectsHiddenAndOutsideChanges(t *testing.T) {
	before := testSnapshot()
	after := testSnapshot()
	targets := map[int]string{1: "Series"}
	after.Collections = append(after.Collections, Collection{ID: "target", Name: "Series", AppIDs: []int{1}})
	after.Collections[0].AppIDs = []int{2, 99}
	after.Collections[1].AppIDs = []int{99}
	a := audit(after, targets, &before)
	if len(a.Problems) > 0 || !a.OutsideUnchanged {
		t.Fatal(a)
	}
	after.Collections[2].AppIDs = []int{99}
	a = audit(after, targets, &before)
	if a.OutsideUnchanged {
		t.Fatal("hidden scope change ignored")
	}
	after = testSnapshot()
	after.Collections[0].AppIDs = []int{1, 2}
	if audit(after, targets, &before).OutsideUnchanged {
		t.Fatal("extra lost ignored")
	}
}
func TestWholeEmbeddedLibrarySimulation(t *testing.T) {
	m, err := manifest()
	if err != nil {
		t.Fatal(err)
	}
	usesPersonalFixture(t, m)
	s := Snapshot{Schema: 1, Account: "123"}
	ids := []int{999999999}
	for i, g := range m.Games {
		s.Apps = append(s.Apps, App{AppID: g.AppID, Name: g.Name})
		if i < 500 {
			ids = append(ids, g.AppID)
		}
	}
	s.Apps = append(s.Apps, App{AppID: 999999999, Name: "Outside manifest"})
	s.Collections = []Collection{{ID: "old", Name: "Existing", Editable: true, Deletable: true, AppIDs: ids}, {ID: "favorite", Name: "Favorites", System: true, Editable: true, AppIDs: []int{m.Games[0].AppID, 999999999}}, {ID: "hidden", Name: "Hidden", System: true, AppIDs: []int{m.Games[1].AppID}}}
	addUserOverlays(m, &s)
	f := &memoryEvaluator{State: s}
	e := &Engine{Eval: f}
	p := buildPreview(m, s, false)
	if len(p.Blocked) > 0 || p.ScopeCount != 5952 {
		t.Fatal(p.ScopeCount, p.Blocked)
	}
	for i, a := range p.Actions {
		if _, err = e.put(a, "full", i); err != nil {
			t.Fatal(err)
		}
	}
	a := audit(f.State, p.Targets, &s, m.Overlays)
	if len(a.Verified) != 5952 || len(a.Problems) > 0 || !a.OutsideUnchanged {
		t.Fatal(a.Scope, len(a.Verified), len(a.Problems), a.OutsideUnchanged)
	}
	p = buildPreview(m, f.State, false)
	if len(p.Actions) != 0 {
		t.Fatal("full second run not idempotent", len(p.Actions))
	}
}
