package main

import (
	_ "embed"
	"strings"
)

// 页面源码拆在 web/ 下，构建时嵌入二进制：
//
//	web/index.html  骨架，留 {{CSS}} / {{JS}} 两个占位
//	web/app.css     样式，深色为默认，html[data-theme=light] 覆盖
//	web/app.js      单页脚本
//
// 拆开的目的是能单独编辑、语法高亮、按文件 diff；产物仍是同一个自包含页面，
// 不新增静态资源路由，浏览器模式和桌面 WebView 下发的 HTML 完全一致。
// 三个 go:embed 分开写，缺文件时直接编译失败，不会拖到运行时才白屏。

//go:embed web/index.html
var indexHTML string

//go:embed web/app.css
var appCSS string

//go:embed web/app.js
var appJS string

// 占位符写成常量，替换按字面量处理，不经过模板或正则引擎。
const (
	cssSlot = "{{CSS}}"
	jsSlot  = "{{JS}}"
)

// guiHTML 是最终下发的整页。包初始化时拼一次，之后每次请求直接复用。
var guiHTML = buildPage()

func buildPage() string {
	page := strings.Replace(indexHTML, cssSlot, appCSS, 1)
	return strings.Replace(page, jsSlot, appJS, 1)
}
