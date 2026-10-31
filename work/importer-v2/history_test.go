package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalHistoryExportImportMoveAndDelete(t *testing.T) {
	home := t.TempDir()
	m := testManifest()
	if _, err := saveHistory(home, "外置清单", "classification", "classification.json", m); err != nil {
		t.Fatal(err)
	}
	items, err := listHistory(home)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	b, err := os.ReadFile(items[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := normalizeImport(b, Snapshot{})
	if err != nil || manifestDigest(parsed) != manifestDigest(m) {
		t.Fatal(err)
	}
	newHome := filepath.Join(t.TempDir(), "moved")
	if err = os.Rename(home, newHome); err != nil {
		t.Fatal(err)
	}
	home = newHome
	items, err = listHistory(home)
	if err != nil || len(items) != 1 || filepath.Dir(filepath.Dir(items[0].Path)) != historyRoot(home) {
		t.Fatal("history did not move with tool", items, err)
	}
	if err = deleteHistory(home, items[0].ID); err != nil {
		t.Fatal(err)
	}
	items, _ = listHistory(home)
	if len(items) != 0 {
		t.Fatal("deleted bundled manifest returned")
	}
	if err = deleteHistory(home, "../backups"); err == nil {
		t.Fatal("path traversal allowed")
	}
	if _, err = saveHistory(home, "bad", "../bad", "file.json", m); err == nil {
		t.Fatal("bad type allowed")
	}
}

func TestSnapshotImportRestoresStaticOverlapAndPreservesArbitraryDynamics(t *testing.T) {
	original := testSnapshot()
	original.Collections = append(original.Collections, Collection{ID: "second", Name: "Second", Editable: true, AppIDs: []int{1, 3}}, Collection{ID: "custom", Name: "我的动态 - 任意名字", Dynamic: true, AppIDs: []int{1, 2, 99}, Filter: json.RawMessage(`{"custom_filter":true}`)})
	raw, _ := json.Marshal(original)
	m, err := normalizeImport(raw, original)
	if err != nil {
		t.Fatal(err)
	}
	if m.CollectionState == nil || m.GameCount != 5 {
		t.Fatal(m)
	}
	f := &memoryEvaluator{State: original}
	e := testEngine(t, f)
	// Simulate reorganizing the library, but preserve the dynamic collection.
	f.State.Collections = append([]Collection{}, original.Collections...)
	f.State.Collections[0].AppIDs = []int{99}
	f.State.Collections[1].AppIDs = []int{99}
	f.State.Collections[3].AppIDs = []int{2}
	f.State.Collections = append(f.State.Collections, Collection{ID: "new", Name: "Newly created", Editable: true, AppIDs: []int{1, 2, 4, 500}})
	f.State.Apps = append(append([]App{}, f.State.Apps...), App{AppID: 500, Name: "New game outside snapshot"})
	p := buildPreview(m, f.State, false)
	if len(p.Blocked) > 0 {
		t.Fatal(p.Blocked)
	}
	for _, a := range p.Actions {
		if a.ID == "custom" || a.Freeze || a.Overlay {
			t.Fatal("dynamic write", a)
		}
	}
	if _, err = e.Apply(m, false, func(Preview) bool { return true }); err != nil {
		t.Fatal(err)
	}
	a := auditPreview(f.State, p, nil)
	if len(a.Problems) > 0 || len(a.OverlayProblems) > 0 {
		t.Fatal(a)
	}
	p = buildPreview(m, f.State, false)
	if len(p.Actions) != 0 {
		t.Fatal("snapshot reimport was not idempotent", p.Actions)
	}
}

func TestSnapshotImportBlocksOtherAccountAndDynamicNameCollision(t *testing.T) {
	s := testSnapshot()
	m := snapshotManifest(s)
	other := testSnapshot()
	other.Account = "456"
	if len(buildPreview(m, other, false).Blocked) == 0 {
		t.Fatal("other account allowed")
	}
	live := testSnapshot()
	live.Collections[0].Dynamic = true
	if len(buildPreview(m, live, false).Blocked) == 0 {
		t.Fatal("would overwrite dynamic collection")
	}
}

func TestSnapshotDeletesObsoleteEmptyCollectionAndPreservesDesiredEmpty(t *testing.T) {
	s := testSnapshot()
	s.Collections = append(s.Collections,
		Collection{ID: "obsolete", Name: "Old empty", Editable: true, Deletable: true, AppIDs: []int{}},
		Collection{ID: "desired", Name: "Keep empty", Editable: true, Deletable: true, AppIDs: []int{}},
	)
	want := s
	want.Collections = append([]Collection{}, s.Collections[:len(s.Collections)-2]...)
	want.Collections = append(want.Collections, s.Collections[len(s.Collections)-1])
	m := snapshotManifest(want)
	p := buildPreview(m, s, false)
	if len(p.Blocked) > 0 || len(p.Actions) != 1 || p.Actions[0].Phase != "delete_collection" || p.Actions[0].ID != "obsolete" {
		t.Fatal(p.Actions, p.Blocked)
	}
	f := &memoryEvaluator{State: s}
	e := testEngine(t, f)
	if _, err := e.put(p.Actions[0], "test", 1); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.State.Collections {
		if c.ID == "obsolete" {
			t.Fatal("obsolete empty collection retained")
		}
	}
	if next := buildPreview(m, f.State, false); len(next.Actions) != 0 || len(next.Blocked) != 0 {
		t.Fatal(next)
	}
}

func TestDynamicAuditDetectsChangedUserFilterOrMissingCollection(t *testing.T) {
	before := testSnapshot()
	before.Collections = append(before.Collections, Collection{ID: "d", Name: "User dynamic", Dynamic: true, AppIDs: []int{1}, Filter: json.RawMessage(`{"arbitrary":1}`)})
	after := before
	after.Collections = append([]Collection{}, before.Collections...)
	after.Collections[len(after.Collections)-1].Filter = json.RawMessage(`{"arbitrary":2}`)
	a := audit(after, map[int]string{}, &before)
	if len(a.OverlayProblems) == 0 {
		t.Fatal("changed filter ignored")
	}
}

func TestMajorDelistedSeriesHasPriorityButSmallSeriesNotPromoted(t *testing.T) {
	m, err := manifest()
	if err != nil {
		t.Fatal(err)
	}
	major := map[int]bool{202970: true, 202990: true, 1190460: true, 6550: true, 321040: true, 1236300: true}
	for _, g := range m.Games {
		if major[g.AppID] && (g.Target == "绝版游戏" || !g.KeepPrimary || !g.Protect) {
			t.Fatal(g)
		}
		if g.AppID == 385200 && g.Target != "绝版游戏" {
			t.Fatal("small series incorrectly promoted", g)
		}
	}
	if len(m.Overlays) != 0 {
		t.Fatal("bundled manifest prescribes dynamic collections")
	}
}
