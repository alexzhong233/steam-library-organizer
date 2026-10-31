package main

// 这一份是 CLI 与桌面两个入口共用的东西，所以不能带构建标签。
// 单独拆出来是因为 main.go 加了 //go:build !desktop：里面放共享定义的话，
// 桌面构建会直接 `undefined`。

import (
	_ "embed"
	"fmt"
	"net"
	"strconv"
	"time"
)

//go:embed bridge.js
var bridgeSource string

// defaultCDPPort 是 Steam CEF 调试端口的默认值。CLI 用它当 flag 默认值，
// 桌面版没有 flag，必须显式传它——v2.4.2 及之前桌面版传的是 0，
// 导致 connect 请求 http://127.0.0.1:0/json，永远连不上 Steam。
const defaultCDPPort = 8080

// normalizeCDPPort 兜底：任何入口传 0 都按默认端口处理。
func normalizeCDPPort(port int) int {
	if port == 0 {
		return defaultCDPPort
	}
	return port
}

// version 用 var 而非 const，便于发布时用
// -ldflags "-X main.version=2.3.4" 注入真实 tag，保证二进制与 Release 一致。
var version = "2.3.2"

func steam64(account string) string {
	n, e := strconv.ParseUint(account, 10, 32)
	if e != nil {
		return "未知"
	}
	return strconv.FormatUint(76561197960265728+n, 10)
}

func restart(root string, debug bool, port int) error {
	running, e := steamRunning()
	if e != nil {
		return e
	}
	if running {
		fmt.Println("请求 Steam 正常退出，等待最多 90 秒；不会强制结束进程。")
		if e = requestShutdown(root); e != nil {
			return e
		}
		deadline := time.Now().Add(90 * time.Second)
		for running && time.Now().Before(deadline) {
			time.Sleep(time.Second)
			running, e = steamRunning()
			if e != nil {
				return e
			}
		}
		if running {
			return fmt.Errorf("Steam 仍在运行。请手动退出游戏及 Steam 后重试，未强制结束")
		}
		time.Sleep(2 * time.Second)
	}
	if debug {
		l, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if e != nil {
			return fmt.Errorf("端口 %d 已占用；用 --port 指定可用端口：%w", port, e)
		}
		l.Close()
	}
	if e = launchSteam(root, debug, port); e != nil {
		return e
	}
	if !debug {
		fmt.Println("已请求普通模式启动。等待同步完成；再次验证时用菜单 1 重启到连接模式，再用菜单 6。")
		return nil
	}
	fmt.Println("Steam 正在启动。请登录并打开“库”，连接检测最长等待 60 秒。")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		c, err := connect(port)
		if err == nil {
			var s Snapshot
			err = c.Eval(map[string]string{"op": "snapshot"}, &s)
			c.Close()
			if err == nil {
				fmt.Printf("连接已就绪，库内 %d 个应用。先用菜单 2 预览。\n", len(s.Apps))
				return nil
			}
		}
	}
	return fmt.Errorf("尚未就绪。登录 Steam、打开库并等待载入后可直接用菜单 2；如果端口未开放，请查看使用说明")
}
