package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Engine struct {
	Eval          Evaluator
	Root          string
	Home          string
	Account       string
	Overlays      []Overlay
	AccountReader func() (string, error)
}
type Backup struct {
	Schema   int       `json:"schema"`
	RunID    string    `json:"run_id"`
	Created  string    `json:"created"`
	Account  string    `json:"account"`
	Snapshot Snapshot  `json:"snapshot"`
	Preview  Preview   `json:"preview"`
	Journal  []Journal `json:"journal"`
	Status   string    `json:"status"`
}
type Journal struct {
	Sequence int         `json:"sequence"`
	Action   Action      `json:"action"`
	Status   string      `json:"status"`
	Result   *Collection `json:"result,omitempty"`
	Error    string      `json:"error,omitempty"`
}
type rawCollection struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Added   []int           `json:"added"`
	Removed []int           `json:"removed"`
	Filter  json.RawMessage `json:"filterSpec"`
}

func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	tmp := path + ".tmp"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(append(b, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func (e *Engine) readAccount() (string, error) {
	if e.AccountReader != nil {
		return e.AccountReader()
	}
	return currentAccount()
}
func (e *Engine) Snapshot() (Snapshot, error) {
	var s Snapshot
	if err := e.Eval.Eval(map[string]any{"op": "snapshot", "overlays": e.Overlays}, &s); err != nil {
		return s, err
	}
	current, err := e.readAccount()
	if err != nil {
		return s, err
	}
	if e.Account != "" && e.Account != current {
		return s, errors.New("Steam 当前账号发生变化，已停止")
	}
	e.Account = current
	s.Account = current
	if s.Schema != 1 || len(s.Apps) == 0 {
		return s, errors.New("不支持的库数据格式")
	}
	seen := map[string]bool{}
	for _, c := range s.Collections {
		if c.ID == "" || seen[c.ID] {
			return s, errors.New("收藏 ID 无效或重复，停止写入")
		}
		seen[c.ID] = true
	}
	// Capture exact dynamic filter plus explicit overrides from Steam's own
	// last-flushed cache. A mismatch with the live filter is a hard preflight stop.
	rawPath := filepath.Join(e.Root, "userdata", current, "config", "cloudstorage", "cloud-storage-namespace-1.json")
	if b, err := os.ReadFile(rawPath); err == nil {
		var pairs [][]json.RawMessage
		if json.Unmarshal(b, &pairs) == nil {
			raws := map[string]rawCollection{}
			for _, p := range pairs {
				if len(p) != 2 {
					continue
				}
				var k string
				var v struct {
					Value   string `json:"value"`
					Deleted bool   `json:"is_deleted"`
				}
				if json.Unmarshal(p[0], &k) != nil || !strings.HasPrefix(k, "user-collections.") || json.Unmarshal(p[1], &v) != nil || v.Deleted {
					continue
				}
				var r rawCollection
				if json.Unmarshal([]byte(v.Value), &r) == nil {
					raws[r.ID] = r
				}
			}
			for i, c := range s.Collections {
				if !c.Dynamic {
					continue
				}
				r, ok := raws[c.ID]
				if !ok || r.Name != c.Name || !usableFilter(r.Filter) {
					continue
				}
				if !usableFilter(c.Filter) || !sameFilter(c.Filter, r.Filter) {
					continue
				}
				s.Collections[i].Filter = r.Filter
				s.Collections[i].ExplicitAdded = r.Added
				s.Collections[i].ExplicitRemoved = r.Removed
				s.Collections[i].FilterCaptured = true
			}
		}
	}
	return s, nil
}
func backupRaw(e *Engine, dir string) error {
	source := filepath.Join(e.Root, "userdata", e.Account, "config", "cloudstorage")
	for _, name := range []string{"cloud-storage-namespace-1.json", "cloud-storage-namespace-1.modified.json", "cloud-storage-namespaces.json"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) Apply(m Manifest, pilot bool, confirm func(Preview) bool) (string, error) {
	before, err := e.Snapshot()
	if err != nil {
		return "", err
	}
	p := buildPreview(m, before, pilot)
	if err = writePreview(e.Home, p, before); err != nil {
		return "", err
	}
	if len(p.Blocked) > 0 {
		return "", fmt.Errorf("预检未通过：%s", strings.Join(p.Blocked, "；"))
	}
	if len(p.Actions) == 0 {
		a := auditPreview(before, p, nil)
		writeJSON(filepath.Join(e.Home, "last-audit.json"), a)
		if len(a.Problems) > 0 || len(a.OverlayProblems) > 0 {
			return "", errors.New("没有可执行操作，但唯一归属审计未通过")
		}
		fmt.Println("所有已找到的清单游戏已符合目标，无需写入。")
		return "", nil
	}
	if !confirm(p) {
		return "", errors.New("已取消，未修改任何 Steam 收藏")
	}
	// Re-read after confirmation; the user must not edit the library in parallel.
	check, err := e.Snapshot()
	if err != nil {
		return "", err
	}
	if fingerprint(check) != p.Fingerprint {
		return "", errors.New("预览后收藏/账号/库发生变化，请重新预览")
	}
	refreshed := buildPreview(m, check, pilot)
	if !equalTargets(p.Targets, refreshed.Targets) {
		return "", errors.New("预览后最后游玩信息发生变化，请重新预览")
	}
	id := time.Now().UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(e.Home, "backups", id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err = backupRaw(e, dir); err != nil {
		return dir, fmt.Errorf("备份失败，未写入 Steam：%w", err)
	}
	b := Backup{Schema: 1, RunID: id, Created: time.Now().Format(time.RFC3339), Account: e.Account, Snapshot: before, Preview: p, Journal: []Journal{}, Status: "prepared"}
	path := filepath.Join(dir, "backup.json")
	if err = writeJSON(path, b); err != nil {
		return dir, err
	}
	if err = writeJSON(filepath.Join(e.Home, "last-run.json"), map[string]string{"backup": path, "account": e.Account}); err != nil {
		return dir, err
	}
	if _, err = saveHistory(e.Home, "修改前的 Steam 收藏 · "+e.Account, "collections", "collections.json", before); err != nil {
		return dir, fmt.Errorf("历史备份失败，未修改 Steam：%w", err)
	}
	for i, act := range p.Actions {
		fmt.Printf("[%d/%d] %s %s（%d → %d 款）\n", i+1, len(p.Actions), act.Phase, act.Name, len(act.Before), len(act.After))
		if cur, er := e.readAccount(); er != nil || cur != b.Account {
			return dir, errors.New("账号发生变化；执行已中止，备份保留")
		}
		b.Journal = append(b.Journal, Journal{Sequence: i + 1, Action: act, Status: "started"})
		b.Status = "applying"
		if err = writeJSON(path, b); err != nil {
			return dir, err
		}
		r, er := e.put(act, id, i+1)
		j := &b.Journal[len(b.Journal)-1]
		if er != nil {
			j.Status = "failed"
			j.Error = er.Error()
			b.Status = "partial"
			writeJSON(path, b)
			return dir, fmt.Errorf("在 %s 停止；已完成部分保留，可重新预览或恢复：%w", act.Name, er)
		}
		j.Status = "done"
		j.Result = &r
		if err = writeJSON(path, b); err != nil {
			return dir, err
		}
	}
	after, err := e.Snapshot()
	if err != nil {
		b.Status = "applied_audit_pending"
		writeJSON(path, b)
		return dir, err
	}
	a := auditPreview(after, p, &before)
	if err = writeJSON(filepath.Join(dir, "audit.json"), a); err != nil {
		return dir, err
	}
	writeJSON(filepath.Join(e.Home, "last-audit.json"), a)
	if len(a.Problems) > 0 || len(a.OverlayProblems) > 0 || !a.OutsideUnchanged {
		b.Status = "audit_failed"
		writeJSON(path, b)
		return dir, fmt.Errorf("保存后审计未通过（%d 款归属问题，清单外未变=%v）。不要宣称完成；查看 audit.json 或恢复", len(a.Problems), a.OutsideUnchanged)
	}
	b.Status = "verified_live"
	if err = writeJSON(path, b); err != nil {
		return dir, err
	}
	fmt.Printf("当前 Steam 内存状态验证：%d/%d 款主收藏/动态例外正确；清单外普通收藏成员关系未变。\n", len(a.Verified), a.Scope)
	fmt.Println("仍需正常重启 Steam、等待同步，然后用菜单 6 检查持久化。获取状态待核实的游戏仍放在临时收藏。")
	return dir, nil
}
func equalTargets(a, b map[int]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func (e *Engine) put(a Action, run string, seq int) (Collection, error) {
	if a.Phase == "delete_collection" {
		var result any
		err := e.Eval.Eval(map[string]any{"op": "delete", "id": a.ID, "expected_name": a.Name, "expected_before": a.Before}, &result)
		return Collection{ID: a.ID, Name: a.Name, AppIDs: []int{}}, err
	}
	id := a.ID
	if strings.HasPrefix(id, "@new:") {
		id = ""
	}
	arg := map[string]any{"op": "put", "id": id, "name": a.Name, "expected_before": a.Before, "appids": a.After, "run_id": run, "sequence": seq, "restore_dynamic": a.RestoreDynamic, "filter": a.Filter, "explicit_added": a.ExplicitAdded, "explicit_removed": a.ExplicitRemoved, "freeze_dynamic": a.Freeze, "overlay": a.Overlay, "expected_name": a.BeforeName, "recovery": a.Recovery}
	var c Collection
	err := e.Eval.Eval(arg, &c)
	return c, err
}
func (e *Engine) LastBackup() (string, error) {
	b, err := os.ReadFile(filepath.Join(e.Home, "last-run.json"))
	if err != nil {
		return "", err
	}
	var r map[string]string
	if err = json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	if r["backup"] == "" {
		return "", errors.New("没有可用备份")
	}
	return r["backup"], nil
}
func (e *Engine) Restore(path string, confirm func(Backup) bool) error {
	savedOverlays := e.Overlays
	e.Overlays = nil
	defer func() { e.Overlays = savedOverlays }()
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var b Backup
	if err = json.Unmarshal(raw, &b); err != nil {
		return err
	}
	if b.Schema != 1 || b.Account == "" || len(b.Snapshot.Collections) == 0 && len(b.Preview.Actions) == 0 {
		return errors.New("无效恢复文件")
	}
	s, err := e.Snapshot()
	if err != nil {
		return err
	}
	if s.Account != b.Account {
		return errors.New("备份账号与当前 Steam 账号不符，拒绝恢复")
	}
	if !confirm(b) {
		return errors.New("已取消恢复")
	}
	recovery := filepath.Join(e.Home, "restore-snapshots", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err = writeJSON(filepath.Join(recovery, "snapshot-before-restore.json"), s); err != nil {
		return err
	}
	if err = backupRaw(e, recovery); err != nil {
		return err
	}
	touched := map[string]bool{}
	touchedIDs := map[string]bool{}
	for _, j := range b.Journal {
		touched[j.Action.Name] = true
		if j.Action.BeforeName != "" {
			touched[j.Action.BeforeName] = true
		}
		if j.Action.ID != "" {
			touchedIDs[j.Action.ID] = true
		}
		if j.Result != nil {
			touchedIDs[j.Result.ID] = true
		}
	}
	// Include the current operation's staging name, even if the process died
	// after the Steam save and before its journal result reached disk.
	originals := map[string]Collection{}
	for _, c := range b.Snapshot.Collections {
		originals[c.ID] = c
	}
	seq := 0
	for _, old := range b.Snapshot.Collections {
		if !touched[old.Name] && !touchedIDs[old.ID] {
			continue
		}
		if old.System && old.ID != "favorite" {
			continue
		}
		if old.Dynamic && !old.FilterCaptured {
			return errors.New("原动态收藏规则未完整保存，停止自动恢复；请查看备份")
		}
		seq++
		current, er := e.Snapshot()
		if er != nil {
			return er
		}
		var found *Collection
		for _, c := range current.Collections {
			if c.ID == old.ID {
				x := c
				found = &x
				break
			}
		}
		if found == nil {
			matches := []Collection{}
			for _, c := range current.Collections {
				if c.Name == old.Name && !c.System {
					matches = append(matches, c)
				}
			}
			if len(matches) > 1 {
				return fmt.Errorf("恢复遇到同名歧义：%s", old.Name)
			}
			if len(matches) == 1 {
				found = &matches[0]
			}
		}
		before := []int{}
		id := ""
		if old.ID == "favorite" {
			id = "favorite"
		}
		if found != nil {
			before = found.AppIDs
			id = found.ID
		}
		if found != nil && found.Name == old.Name && !old.Dynamic && !found.Dynamic && sameIDs(before, old.AppIDs) {
			continue
		}
		beforeName := ""
		freeze := false
		if found != nil {
			beforeName = found.Name
			freeze = found.Dynamic
		}
		a := Action{Name: old.Name, BeforeName: beforeName, Recovery: true, Freeze: freeze, ID: id, Before: before, After: old.AppIDs, RestoreDynamic: old.Dynamic, Filter: old.Filter, ExplicitAdded: old.ExplicitAdded, ExplicitRemoved: old.ExplicitRemoved}
		fmt.Println("恢复收藏：", old.Name)
		if _, err = e.put(a, "restore_"+b.RunID, seq); err != nil {
			return fmt.Errorf("恢复中断，恢复前快照在 %s：%w", recovery, err)
		}
	}
	current, err := e.Snapshot()
	if err != nil {
		return err
	}
	originalNames := map[string]bool{}
	for _, c := range b.Snapshot.Collections {
		originalNames[c.Name] = true
	}
	for _, c := range current.Collections {
		_, existed := originals[c.ID]
		stage := strings.HasPrefix(c.Name, "__SCI_"+b.RunID+"_") || strings.HasPrefix(c.Name, "__SCI_restore_"+b.RunID+"_")
		newlyCreatedTarget := !existed && !originalNames[c.Name] && touched[c.Name]
		if !c.System && (stage || newlyCreatedTarget) {
			fmt.Println("移除本次新增收藏：", c.Name)
			if err = e.Eval.Eval(map[string]any{"op": "delete", "id": c.ID, "expected_name": c.Name}, nil); err != nil {
				return err
			}
		}
	}
	final, err := e.Snapshot()
	if err != nil {
		return err
	}
	// Verify touched original collections by id if retained, exact name otherwise.
	issues := []string{}
	for _, old := range b.Snapshot.Collections {
		if !touched[old.Name] && !touchedIDs[old.ID] {
			continue
		}
		if old.System && old.ID != "favorite" {
			continue
		}
		ok := false
		for _, c := range final.Collections {
			if c.Name == old.Name && c.Dynamic == old.Dynamic && sameIDs(c.AppIDs, old.AppIDs) && (!old.Dynamic || sameFilter(c.Filter, old.Filter)) {
				ok = true
				break
			}
		}
		if !ok {
			issues = append(issues, old.Name)
		}
	}
	if err = writeJSON(filepath.Join(recovery, "restore-result.json"), map[string]any{"issues": issues, "snapshot": final, "source_backup": path}); err != nil {
		return err
	}
	if len(issues) > 0 {
		return fmt.Errorf("恢复后仍有 %d 个收藏不匹配，见 restore-result.json", len(issues))
	}
	fmt.Println("本次修改涉及的原收藏已恢复。新增收藏已清理；动态规则恢复会受当前库内容变化影响。请重启 Steam 后再核对。")
	return nil
}
func summary(p Preview) {
	fmt.Printf("三个动态收藏允许重叠；仅动态归属 %d 款；免费移除候选 %d 款。\n", len(p.DynamicOnly), len(p.FreeCandidates))
	freeze := 0
	for _, a := range p.Actions {
		if a.Freeze {
			freeze++
		}
	}
	fmt.Printf("账号目录 ID：%s（SteamID64：%s）\n清单：%d 款；当前库：%d 款；全量匹配：%d 款\n本次范围：%d 款；未找到：%d 款；写入操作：%d 次；动态转静态：%d 个\n", p.Account, steam64(p.Account), p.ManifestCount, p.LibraryCount, p.FoundTotal, p.ScopeCount, len(p.Missing), len(p.Actions), freeze)
	fmt.Printf("根据新的最后游玩日期调整免费分组：%d 款；获取状态仍待核实：%d 款\n", len(p.Promoted), p.Unresolved)
	for _, s := range p.Blocked {
		fmt.Println("无法执行：", s)
	}
}
func sortedKeys(m map[string]bool) []string {
	a := []string{}
	for k := range m {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
