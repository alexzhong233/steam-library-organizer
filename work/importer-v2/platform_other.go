//go:build !windows

package main

import "fmt"

func currentAccount() (string, error)     { return "", fmt.Errorf("仅支持 Windows Steam") }
func steamRoot() (string, error)          { return "", fmt.Errorf("仅支持 Windows Steam") }
func initConsole()                        {}
func acquireLock() (func(), error)        { return func() {}, nil }
func steamRunning() (bool, error)         { return false, fmt.Errorf("仅支持 Windows Steam") }
func launchSteam(string, bool, int) error { return fmt.Errorf("仅支持 Windows Steam") }
func requestShutdown(string) error        { return fmt.Errorf("仅支持 Windows Steam") }
func openFolder(string) error             { return fmt.Errorf("仅支持 Windows Steam") }
func platformCheck() error {
	return fmt.Errorf("此工具的操作界面仅支持 Windows；其他平台只用于自动测试")
}
