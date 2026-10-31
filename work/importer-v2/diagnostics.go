package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// macOS stores its executable inside the bundle; portable data belongs outside it.
func toolDirectory(executable string) string {
	dir := filepath.Dir(executable)
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		bundle := filepath.Dir(filepath.Dir(dir))
		if strings.HasSuffix(strings.ToLower(bundle), ".app") {
			return filepath.Dir(bundle)
		}
	}
	return dir
}

type DiagnosticStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func diagnoseConnection(port int, home string) []DiagnosticStep {
	steps := []DiagnosticStep{}
	add := func(name, detail string, err error) {
		if err != nil {
			detail = err.Error()
		}
		steps = append(steps, DiagnosticStep{Name: name, OK: err == nil, Detail: detail})
	}
	root, err := steamRoot()
	add("Steam 安装目录", root, err)
	running, err := steamRunning()
	if err == nil && !running {
		err = fmt.Errorf("Steam 未运行；请点击准备连接 Steam")
	}
	add("Steam 进程", "正在运行", err)
	account, err := currentAccount()
	add("当前账号", account, err)
	c, err := connect(port)
	add("本机调试连接", fmt.Sprintf("127.0.0.1:%d", port), err)
	if err == nil {
		defer c.Close()
		eng := Engine{Eval: c, Root: root, Home: home}
		s, snapshotErr := eng.Snapshot()
		add("游戏库与收藏", fmt.Sprintf("%d 个应用，%d 个收藏", len(s.Apps), len(s.Collections)), snapshotErr)
	}
	err = os.MkdirAll(historyRoot(home), 0700)
	if err == nil {
		var file *os.File
		file, err = os.CreateTemp(historyRoot(home), ".connection-check-*")
		if err == nil {
			_, err = file.WriteString("connection-check")
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			removeErr := os.Remove(file.Name())
			if err == nil {
				err = removeErr
			}
		}
	}
	add("历史目录可写", historyRoot(home), err)
	return steps
}
