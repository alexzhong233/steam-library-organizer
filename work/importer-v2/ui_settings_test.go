package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUISettingsRoundTrip(t *testing.T) {
	home := t.TempDir()
	if got := loadUISettings(home); got.Theme != "dark" {
		t.Fatalf("默认主题应为 dark，得到 %q", got.Theme)
	}

	light := "light"
	got, err := mergeUISettings(home, uiSettingsPatch{Theme: &light})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "light" {
		t.Fatalf("合并后主题应为 light，得到 %q", got.Theme)
	}

	// 只改 AI 地址，不能把主题带回去。
	url := "https://api.example.com/v1"
	got, err = mergeUISettings(home, uiSettingsPatch{AIURL: &url})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "light" || got.AIURL != url {
		t.Fatalf("部分更新覆盖了未提交字段：%+v", got)
	}

	// API Key 一并落盘：先存进去，再单独改主题，Key 不能被带丢。
	key := "sk-test-123456"
	got, err = mergeUISettings(home, uiSettingsPatch{APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != key {
		t.Fatalf("API Key 应已保存，得到 %q", got.APIKey)
	}
	got, err = mergeUISettings(home, uiSettingsPatch{Theme: &light})
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != key {
		t.Fatalf("改主题不应影响已存的 API Key：%+v", got)
	}

	// 重新从磁盘读，确认真的落盘了。
	reloaded := loadUISettings(home)
	if reloaded.Theme != "light" || reloaded.AIURL != url || reloaded.APIKey != key {
		t.Fatalf("从磁盘读回不一致：%+v", reloaded)
	}
}

func TestUISettingsRejectsUnknownTheme(t *testing.T) {
	home := t.TempDir()
	weird := "rainbow"
	got, err := mergeUISettings(home, uiSettingsPatch{Theme: &weird})
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "dark" {
		t.Fatalf("未知主题应回落 dark，得到 %q", got.Theme)
	}
}

func TestUISettingsSurvivesBrokenFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "ui-settings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadUISettings(home); got.Theme != "dark" {
		t.Fatalf("坏文件应回落默认值，得到 %+v", got)
	}
}

// 界面上这几个开关是「主题 / AI 浮层 / 全选 / 拖拽」四个功能的入口，
// 被重构掉就会静默失效，所以用测试钉住。
func TestGUIPageHasInteractionHooks(t *testing.T) {
	page := guiPage()
	for _, want := range []string{
		`id="themeToggle"`,   // 主题切换按钮
		`id="aiPanel"`,       // 浮动的 AI 面板
		`id="selectAll"`,     // 表头全选
		`toggleAI()`,         // 工具栏里的入口
		`applyTargetTo(`,     // 拖放与批量共用的归类函数
		`dataTransfer`,       // 拖拽
		`data-theme='light'`, // 浅色主题变量（prettier 会补引号，别写无引号形式）
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("页面里找不到 %q", want)
		}
	}
}

// guiGuard 的方法白名单是真实的攻击面：GET 一旦放开到写接口，
// 跨站页面用一个 <img src> 就能触发读取；反之只读接口忘了加白名单，
// 前端取数会拿到 405（/api/ui-settings 就这么挂过一次）。
func TestGUIGuardMethods(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := guiGuard(next)
	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/api/state", 200},
		{"GET", "/api/history", 200},
		{"GET", "/api/ui-settings", 200},
		{"POST", "/api/ui-settings", 200},
		{"POST", "/api/config", 200},
		{"GET", "/api/config", 405},
		{"GET", "/api/apply", 405},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Host = "127.0.0.1:1234"
		if c.method == http.MethodPost {
			req.Header.Set("Origin", "http://127.0.0.1:1234")
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != c.want {
			t.Errorf("%s %s：得到 %d，应为 %d", c.method, c.path, rr.Code, c.want)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/apply", nil)
	req.Host = "127.0.0.1:1234"
	req.Header.Set("Origin", "http://evil.local")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("外来 Origin 的写请求应 403，得到 %d", rr.Code)
	}
}
