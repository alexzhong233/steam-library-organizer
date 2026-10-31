package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type NativeLicense struct {
	AppID         int    `json:"appid"`
	FreeRemovable bool   `json:"free_removable"`
	MasterAppID   int    `json:"master_appid"`
	Subscribed    bool   `json:"subscribed"`
	Error         string `json:"error,omitempty"`
}
type RemovalChoice struct {
	AppID    int           `json:"appid"`
	Name     string        `json:"name"`
	Eligible bool          `json:"eligible"`
	Reason   string        `json:"reason"`
	Price    PriceInfo     `json:"price"`
	Native   NativeLicense `json:"native"`
}
type RemovalPlan struct {
	Token     string          `json:"token"`
	Account   string          `json:"account"`
	CreatedAt time.Time       `json:"created_at"`
	Items     []RemovalChoice `json:"items"`
}

func removalReason(g Game, app App, price PriceInfo, native NativeLicense) string {
	if regexp.MustCompile(`(?i)\b(demo|benchmark|playtest|test server|public test)\b|试玩|体验版|性能测试`).MatchString(app.Name) {
		return "试玩或测试程序，保留在对应系列/厂商"
	}
	if g.Protect {
		return "清单标记为保护游戏"
	}
	if app.LastPlayed > 0 {
		return "已有最后游玩记录"
	}
	if g.KeepPrimary {
		return "主要系列/保留游戏，不能批量移除"
	}
	if native.Error != "" {
		return "原生许可证读取失败：" + native.Error
	}
	if !native.FreeRemovable || !native.Subscribed || native.MasterAppID != 0 {
		return "Steam 未确认独立的可移除免费许可证，或该内容从属于其他应用"
	}
	if price.Error != "" || !price.Available {
		return "中国区商店无法确认当前可获取"
	}
	if !price.Free || price.OriginalCNY > 0 || price.FinalCNY > 0 {
		return "当前不是免费游戏，或免费标记与付费报价冲突"
	}
	if price.AppType != "game" {
		return "不是已确认的主体游戏；试玩、工具和附属内容跳过"
	}
	return ""
}

func (e *Engine) CheckRemoval(ctx context.Context, ids []int, m Manifest, fetch func(context.Context, int) PriceInfo) (*RemovalPlan, error) {
	ids = normIDs(ids)
	if len(ids) == 0 || len(ids) > 50 {
		return nil, fmt.Errorf("请每批选择 1–50 款游戏")
	}
	s, err := e.Snapshot()
	if err != nil {
		return nil, err
	}
	var licenses struct {
		Items []NativeLicense `json:"items"`
	}
	if err = e.Eval.Eval(map[string]any{"op": "removal-check", "appids": ids}, &licenses); err != nil {
		return nil, err
	}
	known := map[int]Game{}
	owned := map[int]App{}
	native := map[int]NativeLicense{}
	for _, g := range m.Games {
		known[g.AppID] = g
	}
	for _, a := range s.Apps {
		owned[a.AppID] = a
	}
	for _, n := range licenses.Items {
		native[n.AppID] = n
	}
	token := make([]byte, 24)
	if _, err = rand.Read(token); err != nil {
		return nil, err
	}
	p := &RemovalPlan{Token: hex.EncodeToString(token), Account: s.Account, Items: make([]RemovalChoice, len(ids))}
	// Bound store traffic while retaining the selection order in the preview.
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i, id int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			app, ok := owned[id]
			item := RemovalChoice{AppID: id, Name: app.Name, Native: native[id]}
			if !ok {
				item.Reason = "当前库未找到"
			} else {
				item.Price = fetch(ctx, id)
				item.Reason = removalReason(known[id], app, item.Price, item.Native)
				item.Eligible = item.Reason == ""
				if item.Eligible {
					item.Reason = "当前国区免费、无最后游玩记录，Steam 确认可移除免费许可证"
				}
			}
			p.Items[i] = item
		}(i, id)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The confirmation window starts when checks finish, not before slow requests.
	p.CreatedAt = time.Now()
	return p, nil
}

type RemovalResult struct {
	AppID  int    `json:"appid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type RemovalJournal struct {
	Account   string          `json:"account"`
	CreatedAt time.Time       `json:"created_at"`
	Plan      *RemovalPlan    `json:"plan"`
	Results   []RemovalResult `json:"results"`
}

func (e *Engine) RemoveSelected(ctx context.Context, p *RemovalPlan, m Manifest, fetch func(context.Context, int) PriceInfo) ([]RemovalResult, error) {
	if p == nil || time.Since(p.CreatedAt) > 5*time.Minute {
		return nil, fmt.Errorf("移除预览已过期，请重新检查")
	}
	account, err := e.readAccount()
	if err != nil || account != p.Account {
		return nil, fmt.Errorf("账号已变化，请重新检查")
	}
	ids := []int{}
	for _, item := range p.Items {
		if item.Eligible {
			ids = append(ids, item.AppID)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("没有通过检查的免费游戏")
	}
	// Recheck both the live store and native license after user confirmation.
	fresh, err := e.CheckRemoval(ctx, ids, m, fetch)
	if err != nil {
		return nil, err
	}
	if fresh.Account != p.Account {
		return nil, fmt.Errorf("账号已变化，请重新检查")
	}
	for _, item := range fresh.Items {
		if !item.Eligible {
			return nil, fmt.Errorf("%s 状态已变化：%s；本批未执行", item.Name, item.Reason)
		}
	}
	journal := RemovalJournal{Account: p.Account, CreatedAt: time.Now(), Plan: fresh, Results: []RemovalResult{}}
	path := filepath.Join(e.Home, "removals", time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	if err = writeJSON(path, journal); err != nil {
		return nil, fmt.Errorf("无法写入操作日志，未执行：%w", err)
	}
	for _, item := range fresh.Items {
		account, err = e.readAccount()
		if err != nil || account != p.Account {
			return journal.Results, fmt.Errorf("账号发生变化，停止后续移除")
		}
		row := RemovalResult{AppID: item.AppID, Name: item.Name, Status: "started", Detail: "已记录请求；若工具异常退出，请先重新读取 Steam 核实"}
		journal.Results = append(journal.Results, row)
		if err = writeJSON(path, journal); err != nil {
			return journal.Results, err
		}
		var result struct {
			AppID   int  `json:"appid"`
			Removed bool `json:"removed"`
			Result  int  `json:"result"`
		}
		err = e.Eval.Eval(map[string]any{"op": "remove-free-app", "appid": item.AppID, "confirm": "REMOVE_FREE_APP"}, &result)
		row.Status = "removed"
		row.Detail = "Steam 已确认移除免费许可证"
		if err != nil || !result.Removed || result.AppID != item.AppID || result.Result != 1 {
			row.Status = "failed_or_unknown"
			row.Detail = "结果未确认，已停止后续移除，请重新读取核实"
			if err != nil {
				row.Detail += "：" + err.Error()
			}
		}
		journal.Results[len(journal.Results)-1] = row
		if writeErr := writeJSON(path, journal); writeErr != nil {
			return journal.Results, writeErr
		}
		if row.Status != "removed" {
			return journal.Results, fmt.Errorf("%s", row.Detail)
		}
	}
	return journal.Results, nil
}
