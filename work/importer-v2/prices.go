package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type PriceInfo struct {
	Publishers  []string `json:"publishers,omitempty"`
	Developers  []string `json:"developers,omitempty"`
	AppType     string   `json:"app_type,omitempty"`
	AppID       int      `json:"appid"`
	Name        string   `json:"name,omitempty"`
	Available   bool     `json:"available"`
	Free        bool     `json:"free"`
	Currency    string   `json:"currency,omitempty"`
	FinalCNY    float64  `json:"final_cny,omitempty"`
	OriginalCNY float64  `json:"original_cny,omitempty"`
	DiscountPct int      `json:"discount_percent,omitempty"`
	CheckedAt   string   `json:"checked_at"`
	Error       string   `json:"error,omitempty"`
}

func loadPrices(path string) map[int]PriceInfo {
	m := map[int]PriceInfo{}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func fetchPrice(ctx context.Context, id int) PriceInfo {
	result := PriceInfo{AppID: id, CheckedAt: time.Now().Format(time.RFC3339)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://store.steampowered.com/api/appdetails?appids="+strconv.Itoa(id)+"&cc=cn&l=schinese", nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || resp.StatusCode/100 != 2 {
		result.Error = fmt.Sprintf("Steam 商店 HTTP %d", resp.StatusCode)
		return result
	}
	var raw map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Publishers    []string `json:"publishers"`
			Developers    []string `json:"developers"`
			Type          string   `json:"type"`
			Name          string   `json:"name"`
			IsFree        bool     `json:"is_free"`
			PriceOverview *struct {
				Currency    string `json:"currency"`
				Initial     int    `json:"initial"`
				Final       int    `json:"final"`
				DiscountPct int    `json:"discount_percent"`
			} `json:"price_overview"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &raw) != nil {
		result.Error = "Steam 商店返回不是 JSON"
		return result
	}
	entry, ok := raw[strconv.Itoa(id)]
	if !ok || !entry.Success {
		result.Error = "中国区商店无公开报价或已下架"
		return result
	}
	result.Available, result.Free, result.Name = true, entry.Data.IsFree, entry.Data.Name
	result.Publishers, result.Developers, result.AppType = entry.Data.Publishers, entry.Data.Developers, entry.Data.Type
	if entry.Data.PriceOverview != nil {
		result.Currency = entry.Data.PriceOverview.Currency
		result.FinalCNY = float64(entry.Data.PriceOverview.Final) / 100
		result.OriginalCNY = float64(entry.Data.PriceOverview.Initial) / 100
		result.DiscountPct = entry.Data.PriceOverview.DiscountPct
	}
	return result
}

func refreshPrices(ctx context.Context, ids []int, current map[int]PriceInfo, path string) map[int]PriceInfo {
	if len(ids) > 500 {
		ids = ids[:500]
	}
	out := map[int]PriceInfo{}
	for k, v := range current {
		out[k] = v
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	workers := 8
	if len(ids) < workers {
		workers = len(ids)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				v := fetchPrice(ctx, id)
				previous := current[id]
				if len(v.Publishers) == 0 {
					v.Publishers = previous.Publishers
				}
				if len(v.Developers) == 0 {
					v.Developers = previous.Developers
				}
				if v.AppType == "" {
					v.AppType = previous.AppType
				}
				mu.Lock()
				out[id] = v
				mu.Unlock()
			}
		}()
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			_ = writeJSON(path, out)
			return out
		}
	}
	close(jobs)
	wg.Wait()
	_ = writeJSON(path, out)
	return out
}

func pricePath(home string) string { return filepath.Join(home, "prices.json") }
