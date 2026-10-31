package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed bridge.js
var bridgeSource string

// version 用 var 而非 const，便于发布时用
// -ldflags "-X main.version=v2.3.3" 注入真实 tag，保证二进制与 Release 一致。
var version = "2.3.2"

func steam64(account string) string {
	n, e := strconv.ParseUint(account, 10, 32)
	if e != nil {
		return "未知"
	}
	return strconv.FormatUint(76561197960265728+n, 10)
}

var input = bufio.NewReader(os.Stdin)

func readLine(prompt string) string {
	fmt.Print(prompt)
	s, e := input.ReadString('\n')
	if e != nil && s == "" {
		return "0"
	}
	return strings.TrimSpace(s)
}
func main() {
	initConsole()
	rootOpt := flag.String("steam-root", "", "Steam 安装目录（默认自动检测）")
	port := flag.Int("port", 8080, "本机 Steam CEF 端口，通常 8080")
	report := flag.String("data-dir", "", "数据目录（默认在 EXE 或 macOS App 旁）")
	check := flag.Bool("manifest-info", false, "只检查当前分类配置，不连接 Steam")
	gui := flag.Bool("gui", true, "启动图形界面")
	browser := flag.Bool("browser", true, "启动后自动用默认浏览器打开界面（桌面外壳会关掉它）")
	selfTestFlag := flag.Bool("self-test", false, "运行内置自测并退出")
	planPath := flag.String("plan", "", "显式指定分类配置 JSON；默认不加载方案")
	flag.Parse()
	m, configSource, e := loadConfiguredManifest(*planPath)
	if e != nil {
		fmt.Println(e)
		return
	}
	if *check {
		names := map[string]bool{}
		for _, g := range m.Games {
			names[g.Target] = true
		}
		fmt.Printf("v%s: %d games, %d collections, researched %s, SHA256 %s\n", version, m.GameCount, len(names), m.ResearchDate, manifestDigest(m))
		return
	}
	if *selfTestFlag {
		if err := runSelfTest(); err != nil {
			fmt.Println("自测失败：", err)
			os.Exit(1)
		}
		fmt.Println("自测通过")
		return
	}
	if *gui {
		openBrowserOnStart = *browser
		if err := runGUI(m, configSource, *planPath, *port, *report); err != nil {
			fmt.Fprintln(os.Stderr, "GUI 启动失败：", err)
			os.Exit(1)
		}
		return
	}
	fmt.Println("当前分类配置：", configSource)
	fmt.Printf("Steam 收藏整理工具 v%s\n方案：%d 款；资料日期：%s。\n", version, m.GameCount, m.ResearchDate)
	if e = platformCheck(); e != nil {
		fmt.Println(e)
		return
	}
	unlock, e := acquireLock()
	if e != nil {
		fmt.Println(e)
		readLine("按回车退出：")
		return
	}
	defer unlock()
	root := *rootOpt
	if root == "" {
		root, e = steamRoot()
	}
	if e != nil {
		fmt.Println("无法检测 Steam：", e)
		readLine("按回车退出：")
		return
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return
	}
	if _, e = os.Stat(filepath.Join(root, "steam.exe")); e != nil {
		fmt.Println("找不到 steam.exe：", root)
		readLine("按回车退出：")
		return
	}
	exe, e := os.Executable()
	if e != nil {
		fmt.Println(e)
		return
	}
	home := *report
	if home == "" {
		home = toolDirectory(exe)
	}
	home, e = filepath.Abs(home)
	if e != nil {
		return
	}
	if *port < 1024 || *port > 65535 {
		fmt.Println("端口必须在 1024–65535")
		return
	}
	fmt.Println("报告/备份：", home)
	for {
		fmt.Println("\n1 准备连接（正常重启 Steam；先退出正在运行的游戏）\n2 预览 5 款使命召唤\n3 执行 5 款试运行\n4 预览全库分类\n5 执行全库分类\n6 验证上次执行结果（重启后也用它）\n7 恢复备份（可选历史备份）\n8 打开报告与备份目录\n9 正常重启 Steam，关闭调试入口\n10 导出游戏列表、收藏与可编辑分类配置\n11 离线读取本机 Steam 收藏缓存\n12 导出免费移除候选与保护清单（只读）\n0 退出")
		choice := readLine("选择：")
		if choice == "0" {
			return
		}
		if choice == "1" || choice == "9" {
			if readLine("此操作会正常重启 Steam。请先退出游戏，输入 RESTART 继续：") != "RESTART" {
				continue
			}
			e = restart(root, choice == "1", *port)
			if e != nil {
				fmt.Println("失败：", e)
			}
			continue
		}
		if choice == "8" {
			if e = os.MkdirAll(home, 0700); e == nil {
				e = openFolder(home)
			}
			if e != nil {
				fmt.Println(e)
			}
			continue
		}
		if choice == "11" {
			account, er := currentAccount()
			if er != nil {
				account = ""
			}
			v := readLine("Steam userdata 数字账号目录 ID（回车使用 " + account + "）：")
			if v != "" {
				account = v
			}
			dir, er := ExportCache(root, account, home, m)
			if er != nil {
				fmt.Println("读取失败：", er)
			} else {
				fmt.Println("离线缓存已导出：", dir, "（动态成员和完整拥有列表请用在线导出）")
			}
			continue
		}
		if choice != "2" && choice != "3" && choice != "4" && choice != "5" && choice != "6" && choice != "7" && choice != "10" && choice != "12" {
			fmt.Println("请输入菜单数字")
			continue
		}
		c, err := connect(*port)
		if err != nil {
			fmt.Println(err)
			continue
		}
		engine := Engine{Eval: c, Root: root, Home: home, Overlays: m.Overlays}
		switch choice {
		case "2", "4":
			var s Snapshot
			s, err = engine.Snapshot()
			if err == nil {
				p := buildPreview(m, s, choice == "2")
				summary(p)
				err = writePreview(home, p, s)
				if err == nil {
					fmt.Println("完整预览已写入 last-preview.html 和 last-preview.json；此步骤没有修改收藏。")
				}
			}
		case "3", "5":
			var dir string
			dir, err = engine.Apply(m, choice == "3", func(p Preview) bool {
				summary(p)
				fmt.Println("动态收藏由用户在 Steam 维护，工具只读取并保留。低价值游戏命中动态收藏仍保留静态目标；本次只修改清单内游戏的静态收藏归属。")
				return readLine("先保存自动备份再执行。输入 APPLY 继续：") == "APPLY"
			})
			if dir != "" {
				fmt.Println("本次备份：", dir)
			}
		case "10":
			var dir string
			dir, err = engine.ExportLibrary(m)
			if dir != "" {
				fmt.Println("游戏列表与收藏已导出：", dir)
			}
		case "12":
			var dir string
			dir, err = engine.ExportRemovalReview(m)
			if dir != "" {
				fmt.Println("移除候选/保护报告：", dir, "（没有移除任何许可证）")
			}
		case "6":
			err = verifyLast(&engine)
		case "7":
			var path string
			path, err = engine.LastBackup()
			if err != nil {
				path = strings.Trim(readLine("没有最近备份；输入要恢复的 backup.json 完整路径（回车取消）："), "\"")
				if path != "" {
					err = nil
				}
			}
			if err == nil {
				fmt.Println("最近备份：", path)
				other := readLine("回车使用最近备份；或输入其他 backup.json 的完整路径：")
				if other != "" {
					path = strings.Trim(other, "\"")
				}
				err = engine.Restore(path, func(b Backup) bool {
					fmt.Printf("备份时间：%s；账号：%s；状态：%s\n", b.Created, b.Account, b.Status)
					fmt.Println("将恢复此次修改涉及的收藏，覆盖这些收藏之后的手动变更。动态收藏恢复可能改变收藏 ID，首页展架需要重新选择。")
					return readLine("输入 RESTORE 恢复：") == "RESTORE"
				})
			}
		}
		c.Close()
		if err != nil {
			fmt.Println("已停止：", err)
		}
	}
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
func verifyLast(e *Engine) error {
	path, err := e.LastBackup()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var b Backup
	if err = json.Unmarshal(raw, &b); err != nil {
		return err
	}
	s, err := e.Snapshot()
	if err != nil {
		return err
	}
	if s.Account != b.Account {
		return fmt.Errorf("账号与上次执行不符")
	}
	a := auditPreview(s, b.Preview, &b.Snapshot)
	if err = writeJSON(filepath.Join(e.Home, "last-verification.json"), map[string]any{"checked_at": time.Now().Format(time.RFC3339), "backup": path, "audit": a}); err != nil {
		return err
	}
	fmt.Printf("重新读取当前 Steam：%d/%d 款主收藏/动态例外正确；清单外普通收藏成员关系未变=%v\n", len(a.Verified), a.Scope, a.OutsideUnchanged)
	if len(a.Problems) > 0 || len(a.OverlayProblems) > 0 || !a.OutsideUnchanged {
		return fmt.Errorf("验证未通过，查看 last-verification.json")
	}
	fmt.Println("本次检查通过。如果此前已正常重启，此结果同时确认了重启后的归属。完成后可用菜单 9 关闭调试入口。")
	return nil
}
