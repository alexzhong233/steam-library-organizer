package main

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
)

type previewRow struct {
	AppID                 int
	Name, Target, Current string
}

func writePreview(home string, p Preview, s Snapshot) error {
	if e := writeJSON(filepath.Join(home, "last-preview.json"), p); e != nil {
		return e
	}
	names := map[int]string{}
	for _, a := range s.Apps {
		names[a.AppID] = a.Name
	}
	current := map[int]string{}
	for _, c := range s.Collections {
		if c.System && c.ID != "favorite" {
			continue
		}
		for _, id := range c.AppIDs {
			if current[id] != "" {
				current[id] += "；"
			}
			current[id] += c.Name
		}
	}
	rows := []previewRow{}
	for id, target := range p.Targets {
		rows = append(rows, previewRow{id, names[id], target, current[id]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Target == rows[j].Target {
			return rows[i].AppID < rows[j].AppID
		}
		return rows[i].Target < rows[j].Target
	})
	t, e := template.New("preview").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>Steam 分类预览</title><style>body{font:16px system-ui;max-width:1200px;margin:40px auto;padding:0 20px;background:#f5f7fa;color:#152030}table{border-collapse:collapse;width:100%;background:white}td,th{border:1px solid #d7dfe8;padding:10px;text-align:left}th{background:#e4ecf6;position:sticky;top:0}code{word-break:break-all}.warn{color:#a33018}</style><h1>Steam 分类预览</h1><p>这是本地只读报告。账号 {{.P.Account}}；本次 {{.P.ScopeCount}} 款；写入操作 {{len .P.Actions}} 次；未找到 {{len .P.Missing}} 款。</p><p>目标收藏准备完成后，再移出其他用户收藏及最爱；隐藏状态保留。受影响的动态收藏会转成静态。</p>{{range .P.Blocked}}<p class="warn">阻止执行：{{.}}</p>{{end}}<h2>游戏唯一归属</h2><table><tr><th>AppID</th><th>游戏</th><th>目标收藏</th><th>当前收藏</th></tr>{{range .Rows}}<tr><td>{{.AppID}}</td><td>{{.Name}}</td><td>{{.Target}}</td><td>{{.Current}}</td></tr>{{end}}</table><h2>动态转换与其他操作</h2><table><tr><th>阶段</th><th>收藏</th><th>原数量 → 新数量</th><th>动态转静态</th></tr>{{range .P.Actions}}<tr><td>{{.Phase}}</td><td>{{.Name}}</td><td>{{len .Before}} → {{len .After}}</td><td>{{.Freeze}}</td></tr>{{end}}</table><h2>本次未找到</h2>{{range .P.Missing}}<p>{{.AppID}} {{.Name}}</p>{{end}}</html>`)
	if e != nil {
		return e
	}
	path := filepath.Join(home, "last-preview.html")
	f, e := os.Create(path + ".tmp")
	if e != nil {
		return e
	}
	err := t.Execute(f, struct {
		P    Preview
		Rows []previewRow
	}{p, rows})
	ce := f.Close()
	if err != nil {
		return fmt.Errorf("预览生成失败：%w", err)
	}
	if ce != nil {
		return ce
	}
	return os.Rename(path+".tmp", path)
}
