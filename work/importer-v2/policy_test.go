package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func overlayFixture() (Manifest, Snapshot) {
	m := testManifest()
	m.Overlays = []Overlay{{Name: "00-01｜色情内容", Aliases: []string{"色情内容"}, Filter: json.RawMessage(`{"filterGroups":[{"rgOptions":[1]}]}`)}, {Name: "00-02｜动画色情", Aliases: []string{"动漫色情"}, Filter: json.RawMessage(`{"filterGroups":[{"rgOptions":[2]}]}`)}, {Name: "00-03｜VR 游戏", Aliases: []string{"VR"}, Filter: json.RawMessage(`{"filterGroups":[{"rgOptions":[3]}]}`)}}
	s := testSnapshot()
	s.Apps[1].LastPlayed = 0
	for i, o := range m.Overlays {
		ids := []int{}
		if i < 2 {
			ids = []int{1, 2, 3}
		}
		id := fmt.Sprintf("overlay-%d", i)
		s.Collections = append(s.Collections, Collection{ID: id, Name: o.Name, Dynamic: true, Filter: o.Filter, AppIDs: ids, Editable: true, Deletable: true, FilterCaptured: true})
		s.Projections = append(s.Projections, Projection{Name: o.Name, SourceID: id, Filter: o.Filter, AppIDs: ids})
	}
	m.Games[0].LowValue = true
	m.Games[1].LowValue = true
	m.Games[1].FreeCandidate = true
	m.Games[1].Target = "Free candidates"
	m.Games[2].LowValue = true
	m.Games[2].QualityIndie = true
	m.Games[2].Target = "Quality"
	return m, s
}
func TestOverlayOverlapLowQualityAndFreePolicy(t *testing.T) {
	m, s := overlayFixture()
	p := buildPreview(m, s, false)
	if len(p.Blocked) > 0 {
		t.Fatal(p.Blocked)
	}
	if p.Targets[1] != "Series" || p.Targets[2] != "Free candidates" || p.Targets[3] != "Quality" {
		t.Fatal(p.Targets)
	}
	for _, a := range p.Actions {
		if a.Overlay {
			t.Fatal("dynamic collections must never be written by the organizer")
		}
	}
	f := &memoryEvaluator{State: s}
	e := Engine{Eval: f}
	for i, a := range p.Actions {
		if _, err := e.put(a, "test", i); err != nil {
			t.Fatal(err)
		}
	}
	a := audit(f.State, p.Targets, &s, m.Overlays)
	if len(a.Problems) > 0 || len(a.OverlayProblems) > 0 || !a.OutsideUnchanged {
		t.Fatal(a)
	}
	p = buildPreview(m, f.State, false)
	if len(p.Actions) != 0 {
		t.Fatal("second run changed", p.Actions)
	}
}

func TestDynamicMatchKeepsLowValueStaticAndValuablePrimary(t *testing.T) {
	m, s := overlayFixture()
	m.Games = []Game{
		{AppID: 1, Name: "Low-value game", Target: "低价值游戏", Priority: 5, LowValue: true, DynamicOnlyIfMatched: true},
		{AppID: 2, Name: "Quality indie", Target: "精品小游戏", Priority: 6, QualityIndie: true, DynamicOnlyIfMatched: true},
		{AppID: 3, Name: "Valuable FPS", Target: "系列｜Example FPS", Priority: 1, KeepPrimary: true, DynamicOnlyIfMatched: true},
	}
	m.GameCount = len(m.Games)
	s.Apps = []App{{AppID: 1, Name: "Low-value game"}, {AppID: 2, Name: "Quality indie"}, {AppID: 3, Name: "Valuable FPS"}}
	s.Projections = s.Projections[:len(m.Overlays)]
	for i := range s.Projections {
		s.Projections[i].AppIDs = []int{i + 1}
	}
	p := buildPreview(m, s, false)
	if len(p.Blocked) > 0 {
		t.Fatal(p.Blocked)
	}
	if p.Targets[1] != "低价值游戏" || p.Targets[2] != "精品小游戏" || p.Targets[3] != "系列｜Example FPS" {
		t.Fatalf("dynamic matches lost required static targets: %#v", p.Targets)
	}
}
func TestProtectedFreeNeverBecomesDynamicOnlyOrRemovalCandidate(t *testing.T) {
	m, s := overlayFixture()
	m.Games[0].Protect = true
	m.Games[0].Target = "免费绝版/游玩游戏"
	m.Games[0].FreeCandidate = true
	p := buildPreview(m, s, false)
	if p.Targets[1] == "" {
		t.Fatal("protected lost primary")
	}
	for _, id := range p.FreeCandidates {
		if id == 1 {
			t.Fatal("protected exported candidate")
		}
	}
}
func TestNewManifestUserExamplesAndIndieProtection(t *testing.T) {
	m, err := manifest()
	if err != nil {
		t.Fatal(err)
	}
	usesPersonalFixture(t, m)
	gmap := map[int]Game{}
	for _, g := range m.Games {
		gmap[g.AppID] = g
		if g.QualityIndie && g.LowValue {
			t.Fatal("quality marked low", g.Name)
		}
		if g.FreeCandidate && g.Protect {
			t.Fatal("protected candidate", g.Name)
		}
	}
	for _, id := range []int{304390, 359550} {
		if !strings.Contains(gmap[id].Target, "育碧") {
			t.Fatal(id, gmap[id])
		}
	}
	for _, id := range []int{3910, 8930, 244070, 244090} {
		if !strings.Contains(gmap[id].Target, "Sid Meier") {
			t.Fatal(id, gmap[id])
		}
	}
	for _, id := range []int{413150, 504230, 588650, 367520} {
		g := gmap[id]
		if g.AppID > 0 && (!g.QualityIndie || g.LowValue) {
			t.Fatal("recognized indie missing", g)
		}
	}
	if g := gmap[998930]; strings.Contains(g.Target, "系列") {
		t.Fatal(g)
	}
}
func TestLiveExportsAndOfflineCacheAreReadOnly(t *testing.T) {
	f := &memoryEvaluator{State: testSnapshot()}
	e := testEngine(t, f)
	dir, err := e.ExportLibrary(testManifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"games.json", "collections.json", "library-export.json", "editable-classification.json"} {
		if _, err = os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	m := testManifest()
	m.Games[0].FreeCandidate = true
	m.Games[1].Protect = true
	m.Games[1].ProtectionReasons = []string{"known transition"}
	dir, err = e.ExportRemovalReview(m)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "removal-review.json"))
	if !strings.Contains(string(b), "protected_do_not_remove") || !strings.Contains(string(b), `"execution_supported": false`) {
		t.Fatal(string(b))
	}
	if f.Writes != 0 {
		t.Fatal("export mutated Steam")
	}
	p := filepath.Join(e.Root, "userdata", "123", "config", "cloudstorage", "cloud-storage-namespace-1.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	inner := `{"id":"x","name":"VR","added":[1],"removed":[99],"filterSpec":{"filterGroups":[]}}`
	raw, _ := json.Marshal([][]any{{"user-collections.x", map[string]any{"value": inner}}})
	os.WriteFile(p, raw, 0600)
	dir, err = ExportCache(e.Root, "123", e.Home, m)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "cache-export.json"))
	if !strings.Contains(string(b), `"membership_complete": false`) || !strings.Contains(string(b), `"ownership_proven": false`) {
		t.Fatal(string(b))
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(raw) {
		t.Fatal("cache overwritten")
	}
}
func TestEditableConfigLoadsAndRejectsDuplicates(t *testing.T) {
	m := testManifest()
	p := filepath.Join(t.TempDir(), "classification.json")
	b, _ := json.Marshal(m)
	os.WriteFile(p, b, 0600)
	loaded, _, err := loadConfiguredManifest(p)
	if err != nil || loaded.GameCount != 4 {
		t.Fatal(err)
	}
	m.Games[1].AppID = 1
	b, _ = json.Marshal(m)
	os.WriteFile(p, b, 0600)
	if _, _, err = loadConfiguredManifest(p); err == nil {
		t.Fatal("duplicate allowed")
	}
}

func TestReclassifiedFreePlayedAndDelistedHaveNoDedicatedCollection(t *testing.T) {
	m, err := manifest()
	if err != nil {
		t.Fatal(err)
	}
	usesPersonalFixture(t, m)
	for _, g := range m.Games {
		if strings.Contains(g.Target, "免费绝版") || strings.Contains(g.Target, "免费游玩") {
			t.Fatalf("dedicated free-played collection remains: %+v", g)
		}
		if g.Protect && g.FreeCandidate {
			t.Fatalf("protected app is removable candidate: %+v", g)
		}
	}
	for _, id := range []int{1245610, 2027360, 1519310} { // free history, region/delist protected
		var found bool
		for _, g := range m.Games {
			if g.AppID == id {
				found = true
				if !g.Protect || g.FreeCandidate || g.Target == "绝版游戏" {
					t.Fatalf("free delisted game policy: %+v", g)
				}
			}
		}
		if !found {
			t.Fatalf("missing expected test AppID %d", id)
		}
	}
	for _, g := range m.Games {
		if g.FreeCandidate && g.PlayedTarget == "" {
			t.Fatalf("free candidate has no played fallback: %+v", g)
		}
	}
}
func TestUserCreatedOverlaysRemainUntouchedDuringApplyAndRestore(t *testing.T) {
	m, s := overlayFixture()
	f := &memoryEvaluator{State: s}
	e := testEngine(t, f)
	e.Overlays = m.Overlays
	initial, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := e.Apply(m, false, func(Preview) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Restore(filepath.Join(dir, "backup.json"), func(Backup) bool { return true }); err != nil {
		t.Fatal(err)
	}
	after, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(initial) != fingerprint(after) {
		t.Fatal("Steam-created dynamic collections changed during apply/restore")
	}
}
