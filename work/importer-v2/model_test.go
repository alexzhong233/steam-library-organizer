package main

import (
	"encoding/json"
	"testing"
)

func testManifest() Manifest {
	return Manifest{Schema: 1, GameCount: 4, Games: []Game{{1, "One", "Series", 1}, {2, "Two", "Free", 4}, {3, "Three", "Delisted", 2}, {4, "Four", "Genre", 6}}}
}
func testSnapshot() Snapshot {
	return Snapshot{Schema: 1, Account: "123", Apps: []App{{1, "One", 0}, {2, "Two", 12345}, {3, "Three", 0}, {4, "Four", 0}, {99, "Extra", 0}}, Collections: []Collection{
		{ID: "old", Name: "Old", Editable: true, Deletable: true, AppIDs: []int{1, 2, 99}},
		{ID: "favorite", Name: "Favorites", System: true, Editable: true, AppIDs: []int{1, 99}},
		{ID: "hidden", Name: "Hidden", System: true, AppIDs: []int{2, 99}},
	}}
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
	p := buildPreview(m, s, true)
	if len(p.Targets) != 5 || len(p.Blocked) > 0 {
		t.Fatalf("pilot=%d blocked=%v", len(p.Targets), p.Blocked)
	}
	for _, n := range p.Targets {
		if n != "系列｜Call of Duty（使命召唤）" {
			t.Fatal(n)
		}
	}
}
func TestPriorityAndOutsidePreservation(t *testing.T) {
	s := testSnapshot()
	p := buildPreview(testManifest(), s, false)
	if len(p.Blocked) > 0 || p.Targets[2] != "免费绝版/游玩游戏" {
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
	t.Run("dynamic without backup", func(t *testing.T) {
		s := testSnapshot()
		s.Collections[0].Dynamic = true
		if len(buildPreview(testManifest(), s, false).Blocked) == 0 {
			t.Fatal("allowed dynamic without backup")
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
			if !a.Freeze || !sameFilter(a.Filter, c.Filter) || !sameIDs(a.ExplicitAdded, c.ExplicitAdded) {
				t.Fatal(a)
			}
		}
	}
	if !found {
		t.Fatal("no freeze")
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
	f := &memoryEvaluator{State: s}
	e := &Engine{Eval: f}
	p := buildPreview(m, s, false)
	if len(p.Blocked) > 0 || p.ScopeCount != m.GameCount {
		t.Fatal(p.ScopeCount, p.Blocked)
	}
	for i, a := range p.Actions {
		if _, err = e.put(a, "full", i); err != nil {
			t.Fatal(err)
		}
	}
	a := audit(f.State, p.Targets, &s)
	if len(a.Verified) != m.GameCount || len(a.Problems) > 0 || !a.OutsideUnchanged {
		t.Fatal(a.Scope, len(a.Verified), len(a.Problems), a.OutsideUnchanged)
	}
	p = buildPreview(m, f.State, false)
	if len(p.Actions) != 0 {
		t.Fatal("full second run not idempotent", len(p.Actions))
	}
}
