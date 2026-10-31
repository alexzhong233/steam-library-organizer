//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func steamRoot() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("当前平台未实现 Steam 路径检测")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	p := filepath.Join(home, "Library", "Application Support", "Steam")
	if _, e = os.Stat(p); e != nil {
		return "", e
	}
	return p, nil
}
func currentAccount() (string, error) {
	root, e := steamRoot()
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(filepath.Join(root, "config", "loginusers.vdf"))
	if e != nil {
		return "", fmt.Errorf("无法读取 Steam 最近登录账号：%w", e)
	}
	return accountFromLoginUsers(string(b))
}
func initConsole()                 {}
func acquireLock() (func(), error) { return func() {}, nil }
func steamRunning() (bool, error) {
	if runtime.GOOS != "darwin" {
		return false, fmt.Errorf("当前平台未实现 Steam 进程检测")
	}
	e := exec.Command("pgrep", "-x", "steam_osx").Run()
	return e == nil, nil
}
func launchSteam(root string, debug bool, port int) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("当前平台未实现 Steam 启动")
	}
	args := []string{"-a", "Steam", "--args"}
	if debug {
		args = append(args, "-cef-enable-debugging", fmt.Sprintf("-devtools-port=%d", port))
	}
	output, err := exec.Command("open", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动 Steam 失败：%w；%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
func requestShutdown(root string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("当前平台未实现 Steam 退出")
	}
	// Use Steam's own shutdown request; Apple events may target its launcher
	// instead of the running app bundle and return "user cancelled" (-128).
	path := filepath.Join(root, "Steam.AppBundle", "Steam", "Contents", "MacOS", "steam_osx")
	command := exec.Command(path, "-shutdown")
	if err := command.Start(); err != nil {
		return fmt.Errorf("请求 Steam 退出失败：%w", err)
	}
	return command.Process.Release()
}
func openFolder(p string) error { return exec.Command("open", p).Start() }
func openGUI(url string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}
func platformCheck() error {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		return nil
	}
	return fmt.Errorf("当前平台未实现")
}
