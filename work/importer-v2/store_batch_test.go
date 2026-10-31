package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func intp(v int) *int { return &v }

func TestPriceFromStoreItem(t *testing.T) {
	cases := []struct {
		name    string
		item    storeItem
		wantErr string
		free    bool
		final   float64
		appType string
	}{
		{"免费游戏无主报价", storeItem{AppID: 570, Success: 1, IsFree: true, Type: intp(0)}, "", true, 0, "game"},
		{"付费游戏有主报价", storeItem{AppID: 10, Success: 1, IsFree: false, Type: intp(0),
			BestOption: &storePurchaseOption{FinalPriceInCents: "3500", OriginalPriceInCents: "3500"}}, "", false, 35, "game"},
		{"免费但含可选付费包", storeItem{AppID: 730, Success: 1, IsFree: true, Type: intp(0),
			BestOption: &storePurchaseOption{FinalPriceInCents: "10300"}}, "", true, 103, "game"},
		{"工具不是主体游戏", storeItem{AppID: 228980, Success: 1, IsFree: true, Type: intp(6)}, "", true, 0, "type_6"},
		{"类型缺失按未知处理", storeItem{AppID: 123, Success: 1, IsFree: true}, "", true, 0, "unknown"},
		{"商店无响应", storeItem{AppID: 456, Success: 0}, "中国区商店无公开报价或已下架", false, 0, ""},
	}
	for _, c := range cases {
		got := priceFromStoreItem(c.item)
		if c.wantErr != "" {
			if got.Error != c.wantErr || got.Available {
				t.Fatalf("%s：期望不可用 %q，实得 available=%v error=%q", c.name, c.wantErr, got.Available, got.Error)
			}
			continue
		}
		if got.Error != "" || !got.Available || got.Free != c.free || got.FinalCNY != c.final || got.AppType != c.appType {
			t.Fatalf("%s：实得 available=%v free=%v final=%v type=%q error=%q", c.name, got.Available, got.Free, got.FinalCNY, got.AppType, got.Error)
		}
	}
}

func TestFetchStoreBatchChunksAndParses(t *testing.T) {
	var calls int32
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		raw := r.URL.Query().Get("input_json")
		var payload struct {
			IDs []struct {
				AppID int `json:"appid"`
			} `json:"ids"`
			Context struct {
				CountryCode string `json:"country_code"`
			} `json:"context"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Errorf("input_json 不是 JSON：%v", err)
		}
		if payload.Context.CountryCode != "CN" {
			t.Errorf("国区参数错误：%q", payload.Context.CountryCode)
		}
		sizes = append(sizes, len(payload.IDs))
		items := make([]map[string]any, 0, len(payload.IDs))
		for _, e := range payload.IDs {
			items = append(items, map[string]any{"appid": e.AppID, "success": 1, "is_free": true, "type": 0, "name": fmt.Sprintf("App %d", e.AppID)})
		}
		json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"store_items": items}})
	}))
	defer srv.Close()
	old := storeItemsURL
	storeItemsURL = srv.URL + "/"
	defer func() { storeItemsURL = old }()

	ids := make([]int, 0, 75)
	for i := 1; i <= 75; i++ {
		ids = append(ids, i)
	}
	got, err := fetchStoreBatch(context.Background(), ids)
	if err != nil {
		t.Fatalf("批量获取失败：%v", err)
	}
	if calls != 2 {
		t.Fatalf("75 个 AppID 应分 2 次请求，实得 %d 次（批次大小 %v）", calls, sizes)
	}
	if len(got) != 75 {
		t.Fatalf("应解析 75 条，实得 %d 条", len(got))
	}
	if p := got[42]; p.AppID != 42 || !p.Free || p.AppType != "game" {
		t.Fatalf("条目解析错误：%+v", p)
	}
}

func TestStoreFetcherForReportsUnverifiedOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer srv.Close()
	old := storeItemsURL
	storeItemsURL = srv.URL + "/"
	defer func() { storeItemsURL = old }()

	fetch := storeFetcherFor(context.Background(), []int{10, 20})
	for _, id := range []int{10, 20, 30} {
		p := fetch(context.Background(), id)
		if p.Error == "" || p.Available {
			t.Fatalf("请求失败时必须标记为不可用，实得 %+v", p)
		}
	}
}

func TestStoreBatchPayloadShape(t *testing.T) {
	p := storeBatchPayload([]int{1, 2})
	raw, _ := json.Marshal(p)
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("载荷不可序列化：%v", err)
	}
	if _, ok := back["ids"]; !ok {
		t.Fatalf("载荷缺少 ids：%s", raw)
	}
	if _, err := url.Parse("https://example/?" + url.Values{"input_json": {string(raw)}}.Encode()); err != nil {
		t.Fatalf("载荷无法编码为查询参数：%v", err)
	}
}
