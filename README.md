# Steam 收藏整理工具

这是一个适用于 Windows 和 macOS 的 Steam 收藏整理工具。它可以读取 Steam 游戏库和收藏，导入手动或 AI 生成的分类方案，在独立审核窗口中检查修改，并在保存前自动备份当前收藏。

工具支持 OpenAI 兼容接口、自定义模型、JSON 导入导出、历史记录、商店价格缓存，以及带实时资格检查的免费游戏批量移除。动态收藏只读取，不会被程序静默创建、改名或改写。

当前源码版本 `2.3.2`：免费许可证移除的国区校验从「每款一次 appdetails」改为「每 50 款一次 `IStoreBrowseService/GetItems`」，避开 `store.steampowered.com` 的整机限流，判定语义不变。

## 源码

完整 Go 源码位于 [`work/importer-v2`](work/importer-v2)，当前版本 `2.3.2`，包含 Steam CEF 桥接、内嵌中文网页界面、审核窗口、分类引擎、免费游戏移除、价格缓存和测试，依赖由 `go.mod` 声明。同一份代码两种产物：不带标签是命令行版，`-tags desktop` 是桌面单文件。

**想弄清楚代码怎么跑起来的，看 [`ARCHITECTURE.md`](ARCHITECTURE.md)**：三层结构、读写两条数据流、为什么写入前要重读快照做指纹比对、为什么动态收藏只读。

| 文件 | 作用 |
|---|---|
| `main.go` / `gui.go` / `gui_ui.go` | 入口、本机 HTTP 服务、页面组装（把 `web/` 三个文件拼成一页） |
| `web/index.html` / `web/app.css` / `web/app.js` | 界面源码：页面骨架、样式（深色/浅色两套变量）、单页脚本 |
| `bridge.js` | 在 Steam CEF 里执行的桥接脚本，负责读取库与收藏、写入收藏 |
| `engine.go` / `model.go` / `preview.go` | 计划求值、清单模型、只读预演 |
| `collections_plan.go` | 清单导入：接受规则文件、完整库导出和快照，支持多归属 overlay |
| `policy.go` / `store_batch.go` / `removal.go` | 价值与保护策略、商店批量查询、免费许可证移除 |
| `prices.go` / `history.go` / `export.go` / `accounts.go` | 价格缓存、历史备份、导入导出、账号识别 |

个人 Steam 库快照、个人分类方案、价格缓存和编译产物不会提交到 Git，需要在本机单独提供。没有本机个人清单时，`go test` 会用 `classification.example.json` 顶替夹具，依赖真实 AppID 的断言会自动跳过。

## 测试和手动编译

要求 Go 1.23 或更高版本。在 `work/importer-v2` 目录运行：

```sh
# 核心（命令行）测试；桌面入口的单测单独跑
go test ./...
go test -tags desktop,production ./...

# 命令行版交叉编译，不需要 cgo
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o SteamCollectionImporterCore.exe .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o SteamCollectionImporterCore .
```

仓库中的 `classification.example.json` 只是用于离线测试的五条示例数据。不要把真实 Steam 库导出文件或 API Key 提交到 Git。

## 从源码构建

桌面版和命令行版是**同一份代码**，靠构建标签区分：

```sh
cd work/importer-v2

# 桌面版：Windows 得到单个 exe，macOS 得到 .app（里面只有一个可执行文件）
go build -tags desktop,production -trimpath \
  -ldflags "-s -w -X main.version=2.3.3" -o SteamCollectionImporter .

# 命令行版：不依赖 cgo，任何平台都能交叉编译
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o SteamCollectionImporterCore.exe .
```

桌面版约 12–13 MB，**没有第二个二进制**：`gui.go` 在 `-tags desktop` 下不监听端口，直接把 `http.Handler` 交给 Wails 的 AssetServer，窗口与核心在同一个进程里。命令行版仍然监听 `127.0.0.1` 的随机端口并自动打开浏览器，行为没变。

打包（macOS 会补 `Info.plist` 并做 ad-hoc 签名）：

```sh
python scripts/build_desktop.py --platform windows/amd64 --version 2.3.3
python scripts/build_desktop.py --platform darwin/arm64  --version 2.3.3 --dmg
```

版本号用 `-X main.version=` 注入；不注入时用 `shared.go` 里的默认值。

### 为什么不再有「外壳 + Resources 里的 core」

旧方案是两个二进制：外壳启动 core 子进程、解析它打印的 `GUI: http://127.0.0.1:PORT/`，再用反向代理把页面搬进 WebView。问题不只是「产物有两个文件」——WebView2 与 WKWebView 都不支持 `window.open`，审核窗口要靠注入标记改成浮层，反代还得改写 `Origin` 才能骗过 core 的同源校验。

| 旧方案 | 现在 |
|---|---|
| 外壳 exe + `Resources/SteamCollectionImporterCore` | 一个 exe / 一个 .app |
| 子进程 + 解析 stdout 等端口 | 同进程直接调用 handler |
| 反代改写 Origin 绕开同源校验 | 请求不出进程，无需校验 |
| macOS 上 `wails://` 嵌 `http://` 是混合内容 | 页面就是 WebView 自己的响应 |

唯一保留的约定是首页路径：Wails 的 AssetServer 对 `/` 会截下响应体做 HTML 重排并注入 `runtime.js`，所以 `/` 302 到 `/ui`、`/ui` 再还原成 `/`。这条有单测盯住（`desktop_test.go`）。

首次运行会在 `~/.steam-collection-organizer/` 建数据目录（备份、历史、导出都在这里），不写程序所在目录——单文件分发时程序可能位于 `/Applications` 或只读路径。

## GitHub Actions 自动发布

`.github/workflows/release.yml` 会在推送 `v*` 标签时构建两个包（手动触发只试编译、不发布）：

| 平台 | 产物 |
|---|---|
| Windows x64 | `SteamCollectionImporter-windows-x64.zip` |
| macOS Apple Silicon | `SteamCollectionImporter-macOS-arm64.dmg` / `.zip` |

**不需要配置任何 Secret 或变量。** 工作流在各自平台的 runner 上直接编译：Windows 用 `scripts/build_desktop.py` 出一个 exe，macOS 出 `.app` 并补 `Info.plist` + ad-hoc 签名。产物只有一份二进制，不存在「外壳与核心版本错配」这种问题。

桌面版没有独立的外壳代码：`desktop.go`（`-tags desktop`）和命令行入口在同一个包里，窗口直接拿到核心的 `http.Handler`。唯一的约定是首页路径（`/` → `/ui`），原因和单测见上一节。

版本号由工作流注入 `-ldflags "-X main.version=<tag去v>"`，所以发布出来的二进制里 `-manifest-info` 显示的版本与 Release 标签一致。

发版只有一条路：打 `v*` 标签推上去。

```sh
git tag v2.3.4
git push origin v2.3.4
```

推完去 Actions 看「构建桌面版并发布 Release」，全程约 2 分钟。测试、Windows x64、macOS arm64 三个 job 全绿后才会建 Release 并挂上五个附件；**任何一侧失败都不会建 Release**，得去对应 job 的日志里找原因。

只想验证能不能编译、不真发版时，用 Actions → 构建桌面版并发布 Release → Run workflow（版本号可留空）：只试编译，产物在 Artifacts（需登录、90 天过期），不建 Release、不打标签。

标签不能原地重打：本地已有同名 tag 时 `git tag` 会报错，远端同名 tag 再推也不会触发。要重发得先删掉两边再推新的提交：

```sh
gh release delete v2.3.4 --yes      # 若已建过 Release
git push --delete origin v2.3.4
git tag -d v2.3.4
git tag v2.3.4 && git push origin v2.3.4
```

另外 `.github/workflows/ci.yml` 在每次 push 到 `main` 和每个 PR 上跑：`go vet`、`go test`、桥接脚本测试、五个平台交叉编译、离线自测，以及一条隐私检查——确认个人清单和真实账号没有被误提交。

macOS 包是 ad-hoc 签名（未公证），首次打开若提示「无法验证开发者」，在「系统设置 → 隐私与安全性」点「仍要打开」，或执行：

```sh
xattr -dr com.apple.quarantine /Applications/SteamCollectionImporter.app
```

## 配置

启动工具后，在右侧“AI 整理”面板中填写 OpenAI 兼容 API 根地址、API Key 和模型，然后点击“获取模型”。提示词由用户自行编写，AI 结果会先进入独立审核窗口，确认后才会载入待保存方案。

设置和历史记录默认保存在：

```text
~/.steam-collection-organizer/
```

其中 `settings.json` 保存 AI 地址、模型、主题等界面设置，`history/` 保存历史方案和备份。

## 安全行为

- 动态收藏只读，不会被自动创建、改名或重写。
- 保存收藏前会先创建历史备份，并要求用户明确确认。
- 免费游戏移除会在执行前重新检查中国区免费状态、可再次获取状态和游玩记录。
- 单元测试不会连接 Steam，也不会修改收藏或移除许可证。

本项目采用 MIT License。

## WorkBuddy 接手

本项目可以直接交给 WorkBuddy 继续开发。建议先读 [`ARCHITECTURE.md`](ARCHITECTURE.md) 了解三层结构和读写数据流，再看 `desktop.go`（`-tags desktop` 的入口，窗口如何直接拿到 handler）、`gui.go` 的 `desktopServe` 分支、`scripts/build_desktop.py`（打包脚本）和 `.github/workflows/release.yml`（发布流程）。

本仓库不包含个人 Steam 库、分类方案、价格缓存、历史备份或编译产物。

构建桌面版**不需要任何额外配置**：`release.yml` 在 Windows 与 macOS 各自的 runner 上直接用 `scripts/build_desktop.py` 编出单文件，不存在预先上传 `GUI_CORE_*_URL` 这类步骤，也不需要先准备 core 二进制。发版只需推一个 `v*` 标签。

macOS 侧有两个不能省的细节（`scripts/build_desktop.py` 已经处理好，手编时要自己带上）：WKWebView 走 cgo，必须 `CGO_ENABLED=1`；wails 的文件对话框用了 `UTType`，而它的 `#cgo LDFLAGS` 没声明 `UniformTypeIdentifiers`，不补这个 framework 链接期就报 `Undefined symbols: _OBJC_CLASS_$_UTType`。最低系统版本钉在 11.0（`UTType` 与 Apple Silicon 的共同下限）。

日常提交由 `.github/workflows/ci.yml` 把关：`go vet`、`go test`、桥接脚本测试、五平台交叉编译，以及一条防止个人数据入库的检查。
