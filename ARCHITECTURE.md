# 架构

这份文档说明这个工具**怎么在不修改 Steam 安装、不读本地配置文件的前提下**读写你的收藏，以及各模块的分工。

## 一句话

工具通过 Steam 自带的 CEF 调试端口，在 Steam 自己的页面上下文里执行 JavaScript，调 Steam 前端内部的收藏接口。它不注入 DLL、不改 Steam 文件、不碰 `loginusers.vdf` 之外的任何东西。

## 三层结构

```
┌──────────────────────────────────────────────┐
│  WebView（Wails 外壳 / 浏览器）                │
│  内嵌中文界面，通过 fetch 调 127.0.0.1 的 API   │
└──────────────────┬───────────────────────────┘
                   │ HTTP（仅本机）
┌──────────────────▼───────────────────────────┐
│  Go 核心  work/importer-v2                    │
│  ┌──────────┐ ┌──────────┐ ┌──────────────┐  │
│  │ gui.go   │ │ engine.go│ │ collections_ │  │
│  │ 路由/API  │→│ 快照/写入 │←│ plan.go 校验 │  │
│  └──────────┘ └────┬─────┘ └──────────────┘  │
│                    │ CDP（WebSocket）         │
└────────────────────┼─────────────────────────┘
                     │
┌────────────────────▼─────────────────────────┐
│  Steam 客户端 CEF 调试端口 127.0.0.1:8080      │
│  bridge.js 注入后，在 Steam 页面里执行         │
└──────────────────────────────────────────────┘
```

外壳与核心是两个进程：`desktop-wails` 启动 `core -gui -data-dir <dir>`，从它的 stdout 抓取监听地址，再把 WebView 指向它。所以直接用浏览器打开核心的地址也能用，桌面壳只是个方便的容器。

## 模块分工

### 核心入口

| 文件 | 职责 |
|---|---|
| `main.go` | 命令行入口、参数解析、控制台菜单（保留的老交互方式） |
| `gui.go` | HTTP 服务、约 25 个 `/api/*` 路由、Origin 与本机地址校验 |
| `gui_ui.go` | `//go:embed` 的主界面 HTML（约 34 KB，无外部依赖） |
| `review.go` | 独立审核窗口的 HTML（批量改归属、搜索、分页、接受/放弃草稿） |

### 数据与引擎

| 文件 | 职责 |
|---|---|
| `model.go` | 全部数据结构 + `buildPreview()` 纯函数（把清单和快照算成动作列表）+ `audit()` |
| `engine.go` | `Snapshot()` 读库、`Apply()` 写库、`Restore()` 回滚、备份与 journal |
| `collections_plan.go` | 清单导入归一化、`validateManifest()` 校验、快照模式与单目标模式的切换 |
| `policy.go` | 价值分层（批量上架商、评价数）与保护策略（`protect`、免费候选） |

### 传输与桥接

| 文件 | 职责 |
|---|---|
| `transport.go` | CDP 客户端。连调试端口，`Eval(arg, out)` 把 Go 结构体丢给页面里的 JS 执行 |
| `bridge.js` | 注入 Steam 页面的 `steamImporter(a)`，按 `a.op` 分发到快照/写入/移除等具体实现 |

### 外围能力

| 文件 | 职责 |
|---|---|
| `removal.go` | 免费许可证批量移除，写入前重新校验资格 |
| `store_batch.go` | `IStoreBrowseService/GetItems` 批量取数，每 50 款一次 |
| `prices.go` | 商店价格缓存 |
| `history.go` | 历史备份的列出/读取/删除 |
| `export.go` | 导出游戏列表、收藏与可编辑配置 |
| `diagnostics.go` | 环境诊断（Steam 路径、端口、账号） |
| `accounts.go` | 从 `loginusers.vdf` 识别当前账号 |
| `platform_windows.go` / `platform_other.go` | 平台差异：进程检测、重启 Steam、打开文件夹、控制台 |

## 数据流

### 读库

```
GET /api/preview
  → engine.Snapshot()
      → CDP.Eval({op:"snapshot"})
          → bridge.js 读 Steam 内部 store，返回应用列表 + 全部收藏（含动态收藏的 filter）
  → buildPreview(manifest, snapshot, pilot)
      → 产出 Preview{ Actions[], Targets{}, Blocked[] }
```

`buildPreview` 是**纯函数**：同样的清单和快照必然得到同样的结果，不碰网络也不碰磁盘。这让预览可以反复跑、可以做测试比对。

### 写库

写之前有四道闸：

```
1. buildPreview 产出 Actions
2. len(Blocked) > 0        → 直接拒绝，不写
3. confirm 回调（用户点「确认」）
4. 重新 Snapshot()，比对 fingerprint
      fingerprint 变了 → 「预览后收藏/账号/库发生变化，请重新预览」
      最后游玩时间变了 → 同样要求重新预览
   之后才 backupRaw() 落备份，再逐条 put
```

第 4 步是这套流程里最值得注意的设计：因为你完全可能在预览和确认之间在 Steam 里点了什么。工具不信任「刚才那个快照」，它重读一遍做指纹比对，对不上就整体放弃。

写入的每一步记进 journal，中途崩溃可以用 journal 恢复。

### 动态收藏只读

Steam 有几类收藏是规则驱动的：`00 | VR`、`00｜已本地安装`、`原声音轨`，还有你自己创建的动态收藏（带 filter 的）。这些**只读不写**——工具只在静态收藏里增删成员。因为动态收藏的成员是 Steam 按规则算出来的，写进去也用不了多久就会被覆盖，而且会改掉你精心设的筛选条件。

## 数据结构

`Manifest` 是输入（你想让它变成什么样）：

```go
type Manifest struct {
    CollectionState *Snapshot `json:"collection_state,omitempty"` // 有它就走快照模式
    Schema          int
    ResearchDate    string
    GameCount       int
    Games           []Game
    Overlays        []Overlay  // 多归属补充
    CollectionOrder []string
}
```

两种模式：

- **单目标模式**（`CollectionState == nil`）：每款游戏一个 `Game.Target`，靠 `Overlays` 补多归属。
- **快照模式**（`CollectionState != nil`）：成员关系完全由快照里的 `collections[].appids` 决定，一款游戏可以天然属于多个静态收藏。`Game.Target` 只剩展示作用，但 `validateManifest` 仍要求每款非空且 AppID 唯一。

快照模式是处理「一个游戏同时属于厂商桶和类型桶」的正解，不需要用 overlay 绕。

`Snapshot` 是现场（Steam 现在是什么样），`Preview` 是差异（要做什么），`Action` 是单步操作。

## 安全边界

- **只监听本机**：HTTP 服务绑 `127.0.0.1`，并校验 `Origin`/`Host`，防止浏览器里的恶意页面打本地接口。
- **不用管理员权限**：全程普通用户权限。
- **账号校验**：`Apply` 前比对快照里的账号，账号变了直接停。
- **备份先行**：写入前把涉及的收藏原样存进 `backups/<时间戳>/`，`Restore()` 可按备份回滚。
- **不猜**：找不到、对不上、有歧义，一律报错退出，不做「尽力而为」的写入。

## 构建

核心（纯 Go，唯一依赖 vendored 的 gorilla/websocket）：

```bash
cd work/importer-v2
CGO_ENABLED=0 go build -mod=vendor -trimpath \
  -ldflags "-s -w -X main.version=2.3.2" -o core .
```

版本号是 `var` 不是 `const`，就是为了这一步注入——保证二进制里 `core -manifest-info` 打的版本号和 Release 标签一致。

桌面壳（Wails v2）：

```bash
python scripts/build_wails_release.py --platform darwin/arm64 --core <编译好的 core>
```

`--core` 只是把 core 复制进 App 的 `Resources/`。**core 就是同一份源码编译出来的**，所以 CI 里直接编译即可，不需要从外部下载二进制。

### 外壳怎么把核心页面装进 WebView

外壳自己没有任何界面代码，`desktop-wails` 干三件事：起 core、把 core 的网页反代进系统 WebView、退出时收掉 core。

```go
AssetServer: &assetserver.Options{Handler: shellHandler{proxy: httputil.NewSingleHostReverseProxy(coreURL)}}
```

三处必须注意，否则在壳里一定是白屏或 403：

| 位置 | 现象 | 处理 |
|---|---|---|
| `Origin` 头 | WebView 发的是 `http://wails.localhost`，核心 `guiGuard` 只认自己 → 每个 POST 403 | 反代时统一改写成 `http://<核心 Host>` |
| 首页路径 | Wails 对 `/` 和 `*/index.html` 会截下响应体做 HTML 重排并注入 `/wails/runtime.js` | 首页挂 `/ui`，`/` 302 过去 |
| `window.open` | WebView2 与 WKWebView 都不支持开新窗（wails 两平台都没接 `NewWindowRequested`） | 首页注入 `window.__desktopShell=true`，审核窗口改用内嵌浮层 |

核心起不来时外壳不 panic，而是渲染一张可读的失败页（含手动命令行）。

`desktop-wails/main_test.go` 用假核心把上面三条都钉住了，CI 的 `desktop` job 会跑。

核心侧的配合项：`-browser=false` 让核心不要额外弹一个浏览器标签页（默认仍为 `true`，命令行直跑时行为不变）。

## 测试

```
go test ./...        # 夹具用 classification.example.json；个人清单存在时更严格
node --test bridge_test.mjs
core -self-test      # 不连 Steam，验证预览与保护规则
```

依赖真实 AppID 的断言会在缺少个人清单时自动跳过（`usesPersonalFixture`），所以开源状态下测试全绿。
