package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type memoryEvaluator struct {
	State  Snapshot
	Writes int
	FailAt int
	Next   int
}

func (f *memoryEvaluator) Close() {}
func (f *memoryEvaluator) Eval(arg, out any) error {
	b, _ := json.Marshal(arg)
	var a struct {
		Op, ID, Name string
		AppIDs       []int           `json:"appids"`
		Before       []int           `json:"expected_before"`
		Dynamic      bool            `json:"restore_dynamic"`
		Filter       json.RawMessage `json:"filter"`
	}
	json.Unmarshal(b, &a)
	var result any
	if a.Op == "snapshot" {
		result = f.State
	} else {
		f.Writes++
		if f.FailAt == f.Writes {
			return errors.New("injected client failure")
		}
		i := -1
		for j, c := range f.State.Collections {
			if c.ID == a.ID || (a.ID == "" && c.Name == a.Name) {
				i = j
				break
			}
		}
		if a.Op == "delete" {
			if i >= 0 {
				f.State.Collections = append(f.State.Collections[:i], f.State.Collections[i+1:]...)
			}
			result = map[string]bool{"deleted": true}
		} else {
			if i >= 0 && !sameIDs(f.State.Collections[i].AppIDs, a.Before) {
				return errors.New("concurrent membership changed")
			}
			c := Collection{ID: a.ID, Name: a.Name, AppIDs: a.AppIDs, Dynamic: a.Dynamic, Filter: a.Filter, Editable: true, Deletable: true}
			if i >= 0 {
				c.System = f.State.Collections[i].System
				c.Deletable = f.State.Collections[i].Deletable
				c.Editable = f.State.Collections[i].Editable
			}
			if c.ID == "" || i >= 0 && f.State.Collections[i].Dynamic != a.Dynamic {
				f.Next++
				c.ID = "new-" + string(rune('a'+f.Next))
			}
			if i < 0 {
				f.State.Collections = append(f.State.Collections, c)
			} else {
				f.State.Collections[i] = c
			}
			for j := range f.State.Projections {
				if f.State.Projections[j].Name == c.Name {
					f.State.Projections[j].SourceID = c.ID
					f.State.Projections[j].AppIDs = c.AppIDs
					f.State.Projections[j].Filter = c.Filter
				}
			}
			result = c
		}
	}
	if out != nil {
		b, _ = json.Marshal(result)
		return json.Unmarshal(b, out)
	}
	return nil
}
func testEngine(t *testing.T, f *memoryEvaluator) *Engine {
	t.Helper()
	return &Engine{Eval: f, Root: t.TempDir(), Home: filepath.Join(t.TempDir(), "data"), AccountReader: func() (string, error) { return "123", nil }}
}
func TestApplyIdempotenceRestore(t *testing.T) {
	f := &memoryEvaluator{State: testSnapshot()}
	e := testEngine(t, f)
	initial, _ := e.Snapshot()
	before := fingerprint(initial)
	dir, err := e.Apply(testManifest(), false, func(Preview) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	a := audit(f.State, buildPreview(testManifest(), testSnapshot(), false).Targets, nil)
	if len(a.Problems) > 0 {
		t.Fatal(a)
	}
	writes := f.Writes
	if _, err = e.Apply(testManifest(), false, func(Preview) bool { t.Fatal("idempotence asked for confirmation"); return false }); err != nil {
		t.Fatal(err)
	}
	if f.Writes != writes {
		t.Fatal("idempotent apply wrote")
	}
	if err = e.Restore(filepath.Join(dir, "backup.json"), func(Backup) bool { return true }); err != nil {
		t.Fatal(err)
	}
	final, _ := e.Snapshot()
	if fingerprint(final) != before {
		t.Fatalf("original static state not restored: %+v", f.State.Collections)
	}
}
func TestInterruptedRunJournalAndRecovery(t *testing.T) {
	f := &memoryEvaluator{State: testSnapshot(), FailAt: 2}
	e := testEngine(t, f)
	initial, _ := e.Snapshot()
	before := fingerprint(initial)
	dir, err := e.Apply(testManifest(), false, func(Preview) bool { return true })
	if err == nil {
		t.Fatal("failure ignored")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b Backup
	json.Unmarshal(raw, &b)
	if b.Status != "partial" || len(b.Journal) != 2 || b.Journal[1].Status != "failed" {
		t.Fatal(b.Status, b.Journal)
	}
	f.FailAt = 0
	if err = e.Restore(filepath.Join(dir, "backup.json"), func(Backup) bool { return true }); err != nil {
		t.Fatal(err)
	}
	final, _ := e.Snapshot()
	if fingerprint(final) != before {
		t.Fatal("failed run restore mismatch")
	}
}
func TestBackupFailureAndCancelNeverWrite(t *testing.T) {
	for _, kind := range []string{"cancel", "backup error"} {
		t.Run(kind, func(t *testing.T) {
			f := &memoryEvaluator{State: testSnapshot()}
			e := testEngine(t, f)
			if kind == "backup error" {
				p := filepath.Join(e.Root, "userdata", "123", "config", "cloudstorage", "cloud-storage-namespace-1.json")
				os.MkdirAll(p, 0700)
			}
			_, err := e.Apply(testManifest(), false, func(Preview) bool { return kind != "cancel" })
			if err == nil || f.Writes != 0 {
				t.Fatal("mutated despite cancellation/backup failure", f.Writes, err)
			}
		})
	}
}
func TestAccountChangeStopsBeforeMutation(t *testing.T) {
	f := &memoryEvaluator{State: testSnapshot()}
	e := testEngine(t, f)
	n := 0
	e.AccountReader = func() (string, error) {
		n++
		if n == 1 {
			return "123", nil
		}
		return "456", nil
	}
	_, err := e.Apply(testManifest(), false, func(Preview) bool { return true })
	if err == nil || f.Writes != 0 {
		t.Fatal(err, f.Writes)
	}
}
func TestDynamicCacheMustMatchLiveFilter(t *testing.T) {
	s := testSnapshot()
	s.Collections[0].Dynamic = true
	s.Collections[0].Filter = json.RawMessage(`{"filterGroups":[]}`)
	f := &memoryEvaluator{State: s}
	e := testEngine(t, f)
	p := filepath.Join(e.Root, "userdata", "123", "config", "cloudstorage", "cloud-storage-namespace-1.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	r := rawCollection{ID: "old", Name: "Old", Added: []int{99}, Filter: json.RawMessage(`{"filterGroups":[]}`)}
	inner, _ := json.Marshal(r)
	pairs := [][]any{{"user-collections.old", map[string]any{"value": string(inner)}}}
	b, _ := json.Marshal(pairs)
	os.WriteFile(p, b, 0600)
	snap, err := e.Snapshot()
	if err != nil || !snap.Collections[0].FilterCaptured {
		t.Fatal(err, snap.Collections[0])
	}
	f.State.Collections[0].Filter = json.RawMessage(`{"filterGroups":[{"rgOptions":[9]}]}`)
	snap, err = e.Snapshot()
	if err != nil || snap.Collections[0].FilterCaptured {
		t.Fatal("stale cache allowed", err)
	}
}
