package main

import (
	"strings"
	"testing"
)

// 页面由 web/index.html + app.css + app.js 拼成，改错了不会编译报错，
// 表现只是白屏或样式丢失。这里把最容易踩的几点钉住。

func TestPageHasNoLeftoverSlots(t *testing.T) {
	for _, slot := range []string{cssSlot, jsSlot} {
		if strings.Contains(guiHTML, slot) {
			t.Fatalf("页面里还留着占位符 %s，替换没生效", slot)
		}
	}
}

func TestPageKeepsSingleDocument(t *testing.T) {
	if n := strings.Count(guiHTML, "<html"); n != 1 {
		t.Fatalf("应只有一个 <html>，实际 %d 个", n)
	}
	if n := strings.Count(guiHTML, "</html>"); n != 1 {
		t.Fatalf("应只有一个 </html>，实际 %d 个", n)
	}
	for _, want := range []string{"<style>", "</style>", "<script>", "</script>", "</body>"} {
		if !strings.Contains(guiHTML, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
	// 格式化后 </body> 与 </html> 不再相邻，所以只能比位置不能比整串。
	if strings.LastIndex(guiHTML, "</body>") > strings.LastIndex(guiHTML, "</html>") {
		t.Error("</body> 应出现在 </html> 之前")
	}
}

func TestPageKeepsKeyHooks(t *testing.T) {
	// 这些 id 是前端脚本和 jsdom 测试共用的锚点，改名会静默失效。
	for _, id := range []string{
		`id="themeToggle"`, `id="aiPanel"`, `id="selectAll"`, `id="games"`,
		`id="staticTree"`, `id="bulkTarget"`, `id="collectionMeta"`, `id="status"`,
	} {
		if !strings.Contains(guiHTML, id) {
			t.Errorf("页面缺少 %s", id)
		}
	}
}

func TestAssetsLiveInTheirOwnFiles(t *testing.T) {
	// 特征串挑的是格式化也不会改的部分：CSS 变量名和声明值。
	// 别用 ":root{--bg:" 这种紧凑写法，prettier 一跑就加空格。
	if strings.Contains(indexHTML, "--body-grad:") {
		t.Error("index.html 里还有内联 CSS，样式应只在 web/app.css")
	}
	if strings.Contains(indexHTML, "function renderAll") {
		t.Error("index.html 里还有内联脚本，脚本应只在 web/app.js")
	}
	if !strings.Contains(appCSS, "--body-grad:") || !strings.Contains(appCSS, "color-scheme: light") {
		t.Error("web/app.css 缺少颜色变量或浅色主题覆盖")
	}
	if !strings.Contains(appJS, "function renderAll") || !strings.Contains(appJS, "startup();") {
		t.Error("web/app.js 看着不完整")
	}
}
