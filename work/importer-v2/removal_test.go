package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type removalEvaluator struct {
	memoryEvaluator
	Removed []int
	FailID  int
	PaidID  int
}

func (f *removalEvaluator) Eval(arg, out any) error {
	b, _ := json.Marshal(arg)
	var q struct {
		Op     string `json:"op"`
		AppID  int    `json:"appid"`
		AppIDs []int  `json:"appids"`
	}
	json.Unmarshal(b, &q)
	var result any
	switch q.Op {
	case "removal-check":
		items := []NativeLicense{}
		for _, id := range q.AppIDs {
			items = append(items, NativeLicense{AppID: id, FreeRemovable: id != f.PaidID, Subscribed: true})
		}
		result = map[string]any{"items": items}
	case "remove-free-app":
		f.Removed = append(f.Removed, q.AppID)
		if q.AppID == f.FailID {
			return errors.New("lost reply")
		}
		result = map[string]any{"appid": q.AppID, "removed": true, "result": 1}
	default:
		return f.memoryEvaluator.Eval(arg, out)
	}
	b, _ = json.Marshal(result)
	return json.Unmarshal(b, out)
}
func freeStore(_ context.Context, id int) PriceInfo {
	return PriceInfo{AppID: id, Available: true, Free: true, AppType: "game"}
}

func TestRemovalPreviewBoundsConcurrencyAndStartsExpiryAfterChecks(t *testing.T) {
	f := &removalEvaluator{memoryEvaluator: memoryEvaluator{State: testSnapshot()}}
	ids := []int{11, 12, 13, 14, 15, 16, 17, 18}
	for _, id := range ids {
		f.State.Apps = append(f.State.Apps, App{AppID: id, Name: "Free Game"})
	}
	// 必须注入 AccountReader：否则 Snapshot() 会去调真正的 currentAccount()，
	// 在没有 Steam 的机器（如 CI 的 Linux runner）上直接报"当前平台未实现"。
	e := &Engine{Eval: f, AccountReader: func() (string, error) { return "123", nil }}
	started := make(chan struct{}, len(ids))
	release := make(chan struct{})
	defer close(release)
	var active, peak atomic.Int32
	fetch := func(ctx context.Context, id int) PriceInfo {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		return freeStore(ctx, id)
	}
	type result struct {
		plan *RemovalPlan
		err  error
	}
	done := make(chan result, 1)
	go func() {
		p, err := e.CheckRemoval(context.Background(), ids, Manifest{}, fetch)
		done <- result{p, err}
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("store checks did not run concurrently")
		}
	}
	if peak.Load() != 4 {
		t.Fatal("unexpected concurrency", peak.Load())
	}
	finishedAfter := time.Now()
	// Release each request while keeping the deferred close safe on failure.
	for range ids {
		release <- struct{}{}
	}
	r := <-done
	if r.err != nil || r.plan == nil {
		t.Fatal(r.err)
	}
	if peak.Load() > 4 || r.plan.CreatedAt.Before(finishedAfter) {
		t.Fatal("unbounded traffic or premature expiration", peak.Load(), r.plan.CreatedAt)
	}
	for i, item := range r.plan.Items {
		if item.AppID != ids[i] || !item.Eligible {
			t.Fatal("order or eligibility changed", r.plan.Items)
		}
	}
}

func TestRemovalPreviewCancellationReturnsNoPlan(t *testing.T) {
	f := &removalEvaluator{memoryEvaluator: memoryEvaluator{State: testSnapshot()}}
	// 必须注入 AccountReader：否则 Snapshot() 会去调真正的 currentAccount()，
	// 在没有 Steam 的机器（如 CI 的 Linux runner）上直接报"当前平台未实现"。
	e := &Engine{Eval: f, AccountReader: func() (string, error) { return "123", nil }}
	ctx, cancel := context.WithCancel(context.Background())
	fetch := func(ctx context.Context, id int) PriceInfo {
		cancel()
		return freeStore(ctx, id)
	}
	p, err := e.CheckRemoval(ctx, []int{1, 2, 3}, Manifest{}, fetch)
	if !errors.Is(err, context.Canceled) || p != nil {
		t.Fatal("canceled check returned a usable plan", p, err)
	}
}

func TestRemovalProtectsPaidPlayedRestrictedDemoAndMasterLicense(t *testing.T) {
	for _, kind := range []string{"paid", "played", "restricted", "protected", "master", "demo", "unknown native", "temporary discount"} {
		t.Run(kind, func(t *testing.T) {
			g := Game{}
			a := App{AppID: 1, Name: "Game"}
			p := freeStore(context.Background(), 1)
			n := NativeLicense{AppID: 1, FreeRemovable: true, Subscribed: true}
			switch kind {
			case "paid":
				n.FreeRemovable = false
			case "played":
				a.LastPlayed = 1
			case "restricted":
				p.Available = false
			case "protected":
				g.Protect = true
			case "master":
				n.MasterAppID = 2
			case "demo":
				a.Name = "大作 性能测试工具"
			case "unknown native":
				n = NativeLicense{}
			case "temporary discount":
				p.OriginalCNY = 50
			}
			if removalReason(g, a, p, n) == "" {
				t.Fatal("unsafe removal allowed", kind)
			}
		})
	}
}

func TestRemovalRechecksAndStopsOnChangedStateOrPartialFailure(t *testing.T) {
	for _, kind := range []string{"store changed", "account changed", "partial failure", "success"} {
		t.Run(kind, func(t *testing.T) {
			f := &removalEvaluator{memoryEvaluator: memoryEvaluator{State: testSnapshot()}}
			f.State.Apps[1].LastPlayed = 0
			e := &Engine{Eval: f, Root: t.TempDir(), Home: t.TempDir(), AccountReader: func() (string, error) { return "123", nil }}
			p, err := e.CheckRemoval(context.Background(), []int{1, 2, 3}, testManifest(), freeStore)
			if err != nil {
				t.Fatal(err)
			}
			fetch := freeStore
			if kind == "store changed" {
				fetch = func(ctx context.Context, id int) PriceInfo {
					p := freeStore(ctx, id)
					if id == 2 {
						p.Free = false
					}
					return p
				}
			}
			if kind == "account changed" {
				e.AccountReader = func() (string, error) { return "456", nil }
			}
			if kind == "partial failure" {
				f.FailID = 2
			}
			results, err := e.RemoveSelected(context.Background(), p, testManifest(), fetch)
			if kind == "success" {
				if err != nil || len(results) != 3 {
					t.Fatal(results, err)
				}
			} else if err == nil {
				t.Fatal("failure ignored")
			}
			if kind == "store changed" || kind == "account changed" {
				if len(f.Removed) != 0 {
					t.Fatal("mutated after changed state", f.Removed)
				}
			}
			if kind == "partial failure" {
				if len(f.Removed) != 2 || len(results) != 2 || results[1].Status != "failed_or_unknown" {
					t.Fatal("did not stop after uncertain reply", f.Removed, results)
				}
				logs, _ := filepath.Glob(filepath.Join(e.Home, "removals", "*.json"))
				if len(logs) != 1 {
					t.Fatal(logs)
				}
			}
			items, _ := listHistory(e.Home)
			if len(items) != 0 {
				t.Fatal("license removal must not create collection history")
			}
		})
	}
}

func TestHistoryOnlyWhenCollectionChangesAreApplied(t *testing.T) {
	f := &memoryEvaluator{State: testSnapshot()}
	e := testEngine(t, f)
	if _, err := e.Snapshot(); err != nil {
		t.Fatal(err)
	}
	m := snapshotManifest(f.State)
	buildPreview(m, f.State, false)
	if items, _ := listHistory(e.Home); len(items) != 0 {
		t.Fatal(items)
	}
	if _, err := e.Apply(m, false, func(Preview) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if items, _ := listHistory(e.Home); len(items) != 0 {
		t.Fatal("no-op created backup", items)
	}
	if _, err := e.Apply(testManifest(), false, func(Preview) bool { return false }); err == nil {
		t.Fatal("cancel ignored")
	}
	if items, _ := listHistory(e.Home); len(items) != 0 {
		t.Fatal("cancel created history", items)
	}
	if _, err := e.Apply(testManifest(), false, func(Preview) bool { return true }); err != nil {
		t.Fatal(err)
	}
	items, _ := listHistory(e.Home)
	if len(items) != 1 {
		t.Fatal(items)
	}
	b, _ := os.ReadFile(items[0].Path)
	var saved Snapshot
	if json.Unmarshal(b, &saved) != nil || fingerprint(saved) != fingerprint(testSnapshot()) {
		t.Fatal("wrong before-state backup")
	}
}

func TestUnclassifiedRuleRemovesStaticButKeepsDynamic(t *testing.T) {
	m := testManifest()
	m.Games[0].Target = "未分类"
	s := testSnapshot()
	s.Collections = append(s.Collections, Collection{ID: "d", Name: "Any Dynamic", Dynamic: true, AppIDs: []int{1}})
	f := &memoryEvaluator{State: s}
	e := testEngine(t, f)
	if _, err := e.Apply(m, false, func(Preview) bool { return true }); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.State.Collections {
		if c.Name == "未分类" {
			t.Fatal("created fake uncategorized collection")
		}
		if !c.Dynamic && (!c.System || c.ID == "favorite") {
			for _, id := range c.AppIDs {
				if id == 1 {
					t.Fatal("static membership retained")
				}
			}
		}
	}
	if !sameIDs(f.State.Collections[len(f.State.Collections)-1].AppIDs, []int{1}) {
		for _, c := range f.State.Collections {
			if c.ID == "d" && !sameIDs(c.AppIDs, []int{1}) {
				t.Fatal("dynamic changed")
			}
		}
	}
}

// Optional full-library verification against a personal external plan. No live
// Steam calls or filesystem collection writes occur in this simulation.
func TestExternalPlanPreviewAndSimulation(t *testing.T) {
	path := os.Getenv("STEAM_EXTERNAL_PLAN")
	beforePath := os.Getenv("STEAM_EXTERNAL_BEFORE")
	if path == "" || beforePath == "" {
		t.Skip("external plan paths not supplied")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := normalizeImport(b, Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(beforePath)
	if err != nil {
		t.Fatal(err)
	}
	var source Manifest
	if err = json.Unmarshal(b, &source); err != nil || source.CollectionState == nil {
		t.Fatal(err)
	}
	before := *source.CollectionState
	p := buildPreview(m, before, false)
	if len(p.Blocked) > 0 {
		t.Fatal(p.Blocked)
	}
	f := &memoryEvaluator{State: before}
	e := testEngine(t, f)
	for i, action := range p.Actions {
		if action.Overlay || action.Freeze || action.RestoreDynamic {
			t.Fatal("dynamic mutation", action)
		}
		if _, err = e.put(action, "simulation", i+1); err != nil {
			t.Fatal(err)
		}
	}
	a := auditPreview(f.State, p, &before)
	if len(a.Problems) > 0 || len(a.OverlayProblems) > 0 || !a.OutsideUnchanged {
		t.Fatal("audit failed", a)
	}
	if remaining := buildPreview(m, f.State, false); len(remaining.Actions) > 0 || len(remaining.Blocked) > 0 {
		t.Fatal("not idempotent", len(remaining.Actions), remaining.Blocked)
	}
	t.Logf("%d apps, %d operations, %d verified; dynamic collections unchanged", m.GameCount, len(p.Actions), len(a.Verified))
}
