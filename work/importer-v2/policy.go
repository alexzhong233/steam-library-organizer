package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Overlay struct {
	Name         string          `json:"name"`
	Aliases      []string        `json:"aliases"`
	Filter       json.RawMessage `json:"filter"`
	ManualCreate bool            `json:"manual_create,omitempty"`
	Instruction  string          `json:"instruction,omitempty"`
}
type Projection struct {
	Name     string          `json:"name"`
	SourceID string          `json:"source_id"`
	AppIDs   []int           `json:"appids"`
	Filter   json.RawMessage `json:"filter"`
}

func isOverlayName(name string, defs []Overlay) bool {
	for _, o := range defs {
		if strings.EqualFold(name, o.Name) {
			return true
		}
		for _, a := range o.Aliases {
			if strings.EqualFold(name, a) {
				return true
			}
		}
	}
	return false
}
func prepareOverlays(m Manifest, s Snapshot, p *Preview) ([]Collection, map[int][]string) {
	simulated := append([]Collection{}, s.Collections...)
	membership := map[int][]string{}
	// Dynamic collections are entirely user-defined. Read every dynamic
	// collection reported by Steam and use its current membership only as an
	// overlap signal. The organizer never creates, renames, changes, or deletes
	// these collections, and no JSON-defined names or filters are required.
	for _, c := range s.Collections {
		if c.System || !c.Dynamic {
			continue
		}
		for _, id := range c.AppIDs {
			membership[id] = append(membership[id], c.Name)
		}
	}
	return simulated, membership
}
func validateManifest(m Manifest) error {
	if (m.Schema != 1 && m.Schema != 2) || m.GameCount < 1 || m.GameCount > 100000 || m.GameCount != len(m.Games) {
		return fmt.Errorf("分类配置数量/格式不符")
	}
	seen := map[int]bool{}
	if m.CollectionState != nil {
		names := map[string]bool{}
		for _, c := range m.CollectionState.Collections {
			if c.Dynamic || c.System && c.ID != "favorite" {
				continue
			}
			if strings.TrimSpace(c.Name) == "" || names[c.Name] {
				return fmt.Errorf("快照静态收藏名称为空或重复")
			}
			names[c.Name] = true
			for _, id := range c.AppIDs {
				if id <= 0 {
					return fmt.Errorf("快照 AppID 无效")
				}
			}
		}
	}
	for _, g := range m.Games {
		if g.AppID <= 0 || seen[g.AppID] || strings.TrimSpace(g.Target) == "" || g.Priority < 0 || g.Priority > 6 {
			return fmt.Errorf("分类配置 AppID 重复或字段无效")
		}
		seen[g.AppID] = true
	}
	ns := map[string]bool{}
	for _, o := range m.Overlays {
		if o.Name == "" || ns[o.Name] {
			return fmt.Errorf("动态收藏定义无效")
		}
		ns[o.Name] = true
	}
	aliases := map[string]string{}
	for _, o := range m.Overlays {
		for _, name := range append([]string{o.Name}, o.Aliases...) {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" {
				continue
			}
			if prior, ok := aliases[key]; ok && prior != o.Name {
				return fmt.Errorf("动态收藏别名跨规则重复：%s", name)
			}
			aliases[key] = o.Name
		}
	}
	for _, g := range m.Games {
		if isOverlayName(g.Target, m.Overlays) {
			return fmt.Errorf("游戏主收藏不可直接指定为保留动态收藏，请使用普通后备收藏")
		}
		if g.QualityIndie && (g.LowValue || g.Target == "低价值游戏") {
			return fmt.Errorf("精品小游戏不能同时标为低价值：%s", g.Name)
		}
	}
	return nil
}
