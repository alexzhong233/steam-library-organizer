package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type HistoryItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"path"`
}

func historyRoot(home string) string { return filepath.Join(home, "history") }

func saveHistory(home, title, kind, filename string, value any) (HistoryItem, error) {
	if filename != filepath.Base(filename) || filename == ".." || filename == "." {
		return HistoryItem{}, fmt.Errorf("历史文件名无效")
	}
	if strings.ContainsAny(kind, `/\\`) || strings.Contains(kind, "..") {
		return HistoryItem{}, fmt.Errorf("历史类型无效")
	}
	id := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + kind
	dir := filepath.Join(historyRoot(home), id)
	path := filepath.Join(dir, filename)
	if err := writeJSON(path, value); err != nil {
		return HistoryItem{}, err
	}
	item := HistoryItem{ID: id, Title: title, Kind: kind, CreatedAt: time.Now().Format(time.RFC3339), Path: path}
	stored := item
	stored.Path = filename
	if err := writeJSON(filepath.Join(dir, "history.json"), stored); err != nil {
		return HistoryItem{}, err
	}
	return item, nil
}

func listHistory(home string) ([]HistoryItem, error) {
	entries, err := os.ReadDir(historyRoot(home))
	if os.IsNotExist(err) {
		return []HistoryItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := []HistoryItem{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, er := os.ReadFile(filepath.Join(historyRoot(home), entry.Name(), "history.json"))
		if er != nil {
			continue
		}
		var item HistoryItem
		if json.Unmarshal(b, &item) == nil && item.ID != "" {
			// Rebase older records that stored a path relative to the launch folder.
			item.Path = filepath.Join(historyRoot(home), entry.Name(), filepath.Base(item.Path))
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func deleteHistory(home, id string) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return fmt.Errorf("历史记录编号无效")
	}
	root := historyRoot(home)
	path := filepath.Join(root, id)
	if _, err := os.Stat(filepath.Join(path, "history.json")); err != nil {
		return fmt.Errorf("历史记录不存在")
	}
	return os.RemoveAll(path)
}
