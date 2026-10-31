package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The removal gate used to call store.steampowered.com/api/appdetails once per
// app. That endpoint rate-limits aggressively (HTTP 403 for the whole host after
// a few hundred requests), which made any batch removal unusable. The batch
// endpoint below returns the same facts for 50 appids per request and is not
// affected by that limit, so the gate keeps its meaning while issuing far fewer
// requests. Fields map to the previous appdetails semantics:
//   success != 1        -> 中国区商店无公开报价或已下架
//   is_free             -> Free
//   best_purchase_option-> FinalCNY / OriginalCNY (the app's own price)
//   type                -> AppType, only 0 counts as "game"
const storeBatchSize = 50

// Overridable for tests; production always uses the official endpoint.
var storeItemsURL = "https://api.steampowered.com/IStoreBrowseService/GetItems/v1/"

type storePurchaseOption struct {
	FinalPriceInCents    string `json:"final_price_in_cents"`
	OriginalPriceInCents string `json:"original_price_in_cents"`
	DiscountPct          int    `json:"discount_pct"`
	PackageGroup         string `json:"package_group"`
}

type storeItem struct {
	AppID      int                  `json:"appid"`
	ID         int                  `json:"id"`
	Success    int                  `json:"success"`
	Visible    bool                 `json:"visible"`
	Name       string               `json:"name"`
	Type       *int                 `json:"type"`
	IsFree     bool                 `json:"is_free"`
	BestOption *storePurchaseOption `json:"best_purchase_option"`
	BasicInfo  struct {
		Publishers []struct {
			Name string `json:"name"`
		} `json:"publishers"`
		Developers []struct {
			Name string `json:"name"`
		} `json:"developers"`
	} `json:"basic_info"`
}

func storeAppType(t *int) string {
	if t == nil {
		return "unknown"
	}
	if *t == 0 {
		return "game"
	}
	return fmt.Sprintf("type_%d", *t)
}

func centsToCNY(raw string) float64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n / 100
}

// priceFromStoreItem converts one batch item into the same PriceInfo shape the
// per-app endpoint produced, so removalReason needs no changes.
func priceFromStoreItem(it storeItem) PriceInfo {
	id := it.AppID
	if id == 0 {
		id = it.ID
	}
	p := PriceInfo{AppID: id, CheckedAt: time.Now().Format(time.RFC3339), Name: it.Name}
	if it.Success != 1 {
		p.Error = "中国区商店无公开报价或已下架"
		return p
	}
	p.Available = true
	p.Free = it.IsFree
	p.AppType = storeAppType(it.Type)
	for _, v := range it.BasicInfo.Publishers {
		p.Publishers = append(p.Publishers, v.Name)
	}
	for _, v := range it.BasicInfo.Developers {
		p.Developers = append(p.Developers, v.Name)
	}
	if it.BestOption != nil {
		p.Currency = "CNY"
		p.FinalCNY = centsToCNY(it.BestOption.FinalPriceInCents)
		p.OriginalCNY = centsToCNY(it.BestOption.OriginalPriceInCents)
		p.DiscountPct = it.BestOption.DiscountPct
	}
	return p
}

func storeBatchPayload(ids []int) map[string]any {
	entries := make([]map[string]any, len(ids))
	for i, id := range ids {
		entries[i] = map[string]any{"appid": id}
	}
	return map[string]any{
		"ids":     entries,
		"context": map[string]any{"language": "schinese", "country_code": "CN", "steam_realm": 1},
		"data_request": map[string]any{
			"include_basic_info":           true,
			"include_all_purchase_options": true,
		},
	}
}

// fetchStoreBatch asks the public store for CN availability, one request per 50
// appids. Any transport failure is returned so callers can mark every item as
// unverified instead of guessing.
func fetchStoreBatch(ctx context.Context, ids []int) (map[int]PriceInfo, error) {
	out := map[int]PriceInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for start := 0; start < len(ids); start += storeBatchSize {
		end := start + storeBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		raw, err := json.Marshal(storeBatchPayload(ids[start:end]))
		if err != nil {
			return out, err
		}
		target := storeItemsURL + "?input_json=" + url.QueryEscape(string(raw))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("User-Agent", "SteamCollectionImporter/"+version)
		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if readErr != nil {
			return out, readErr
		}
		if resp.StatusCode/100 != 2 {
			return out, fmt.Errorf("Steam 批量商店接口 HTTP %d", resp.StatusCode)
		}
		var parsed struct {
			Response struct {
				Items []storeItem `json:"store_items"`
			} `json:"response"`
		}
		if err = json.Unmarshal(body, &parsed); err != nil {
			return out, fmt.Errorf("Steam 批量商店接口返回不是 JSON：%w", err)
		}
		for _, it := range parsed.Response.Items {
			p := priceFromStoreItem(it)
			out[p.AppID] = p
		}
		if start+storeBatchSize < len(ids) {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return out, nil
}

// storeFetcherFor prefetches CN store data for the given appids and returns the
// per-app lookup that CheckRemoval / RemoveSelected expect. Apps absent from the
// response are reported as unverified, so the gate skips them.
func storeFetcherFor(ctx context.Context, ids []int) func(context.Context, int) PriceInfo {
	cache, err := fetchStoreBatch(ctx, ids)
	return func(_ context.Context, id int) PriceInfo {
		if p, ok := cache[id]; ok {
			return p
		}
		p := PriceInfo{AppID: id, CheckedAt: time.Now().Format(time.RFC3339)}
		if err != nil {
			p.Error = "国区批量资料获取失败：" + err.Error()
		} else {
			p.Error = "中国区商店无公开报价或已下架"
		}
		return p
	}
}
