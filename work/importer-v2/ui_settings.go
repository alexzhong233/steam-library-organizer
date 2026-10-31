package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// uiSettings 是界面自己的偏好，和分类清单（manifest）没有关系。
//
// 为什么不只放 localStorage：桌面版的 WebView 数据目录会随程序升级、清缓存
// 或换机器而消失，用户不该每次都重新选主题、重填 AI 地址和模型。
//
// API Key 按用户要求一并落盘（文件权限 0600，目录 0700，仅本机可读）。
// 它明文存在数据目录里，风险是本机其他进程可读——换来的是升级或换机后
// 不用重新填。桌面单文件场景这个取舍用户已确认接受。
type uiSettings struct {
	Theme  string `json:"theme"`
	AIURL  string `json:"ai_url"`
	Model  string `json:"model"`
	APIKey string `json:"api_key"`
}

// uiSettingsMu 只保护这个文件的读写，不与 guiState.mu 抢锁。
var uiSettingsMu sync.Mutex

func uiSettingsPath(home string) string {
	return filepath.Join(home, "ui-settings.json")
}

func loadUISettings(home string) uiSettings {
	s := uiSettings{Theme: "dark"}
	if b, err := os.ReadFile(uiSettingsPath(home)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Theme != "light" {
		s.Theme = "dark"
	}
	return s
}

func saveUISettings(home string, s uiSettings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(uiSettingsPath(home), append(b, '\n'), 0o600)
}

// uiSettingsPatch 用指针区分「没传这个字段」和「传了空字符串」：
// 前者保持原值（切主题时不必把 AI 地址一起回传），后者是真的要清空。
type uiSettingsPatch struct {
	Theme  *string `json:"theme"`
	AIURL  *string `json:"ai_url"`
	Model  *string `json:"model"`
	APIKey *string `json:"api_key"`
}

// mergeUISettings 把 patch 合并进磁盘上的当前值并写回。
func mergeUISettings(home string, patch uiSettingsPatch) (uiSettings, error) {
	uiSettingsMu.Lock()
	defer uiSettingsMu.Unlock()
	cur := loadUISettings(home)
	if patch.Theme != nil {
		if *patch.Theme == "light" {
			cur.Theme = "light"
		} else {
			cur.Theme = "dark"
		}
	}
	if patch.AIURL != nil {
		cur.AIURL = strings.TrimSpace(*patch.AIURL)
	}
	if patch.Model != nil {
		cur.Model = strings.TrimSpace(*patch.Model)
	}
	if patch.APIKey != nil {
		cur.APIKey = strings.TrimSpace(*patch.APIKey)
	}
	return cur, saveUISettings(home, cur)
}
