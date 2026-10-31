# Steam 收藏整理工具

这是一个适用于 Windows 和 macOS 的 Steam 收藏整理工具。它可以读取 Steam 游戏库和收藏，导入手动或 AI 生成的分类方案，在独立审核窗口中检查修改，并在保存前自动备份当前收藏。

工具支持 OpenAI 兼容接口、自定义模型、JSON 导入导出、历史记录、商店价格缓存，以及带实时资格检查的免费游戏批量移除。动态收藏只读取，不会被程序静默创建、改名或改写。

当前源码版本 `2.3.2`：免费许可证移除的国区校验从「每款一次 appdetails」改为「每 50 款一次 `IStoreBrowseService/GetItems`」，避开 `store.steampowered.com` 的整机限流，判定语义不变。

## 源码

完整 Go 源码位于 [`work/importer-v2`](work/importer-v2)，当前版本 `2.3.2`，包含 Steam CEF 桥接、内嵌中文网页界面、审核窗口、分类引擎、免费游戏移除、价格缓存和测试，依赖随 `vendor/` 提供。`desktop-wails/` 是 Wails 桌面外壳，`mac_wrapper.go` 是 macOS ARM64 启动器和主题代理。

**想弄清楚代码怎么跑起来的，看 [`ARCHITECTURE.md`](ARCHITECTURE.md)**：三层结构、读写两条数据流、为什么写入前要重读快照做指纹比对、为什么动态收藏只读。

| 文件 | 作用 |
|---|---|
| `main.go` / `gui.go` / `gui_ui.go` | 入口、本机 HTTP 服务、内嵌网页界面 |
| `bridge.js` | 在 Steam CEF 里执行的桥接脚本，负责读取库与收藏、写入收藏 |
| `engine.go` / `model.go` / `preview.go` | 计划求值、清单模型、只读预演 |
| `collections_plan.go` | 清单导入：接受规则文件、完整库导出和快照，支持多归属 overlay |
| `policy.go` / `store_batch.go` / `removal.go` | 价值与保护策略、商店批量查询、免费许可证移除 |
| `prices.go` / `history.go` / `export.go` / `accounts.go` | 价格缓存、历史备份、导入导出、账号识别 |

个人 Steam 库快照、个人分类方案、价格缓存和编译产物不会提交到 Git，需要在本机单独提供。没有本机个人清单时，`go test` 会用 `classification.example.json` 顶替夹具，依赖真实 AppID 的断言会自动跳过。

## 测试和手动编译

要求 Go 1.23 或更高版本。在 `work/importer-v2` 目录运行：

```sh
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor -o SteamCollectionImporter.exe .
GOOS=windows GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter-arm64.exe .
GOOS=darwin GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter .
```

仓库中的 `classification.example.json` 只是用于离线测试的五条示例数据。不要把真实 Steam 库导出文件或 API Key 提交到 Git。

## 从源码构建 GUI 核心

桌面外壳本身只有窗口和 WebView，真正的界面与 Steam 操作都在 `SteamCollectionImporterCore` 里。核心可以直接从本仓库构建，三个平台都能交叉编译，无需 cgo：

```sh
cd work/importer-v2
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -mod=vendor -o SteamCollectionImporterCore.exe .
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporterCore-arm64.exe .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporterCore .
```

产物约 10 MB（加 `-ldflags "-s -w"`）或 14–15 MB（不裁剪）。`SteamCollectionImporter` 这个名字留给外壳，核心统一叫 `SteamCollectionImporterCore`，`scripts/build_wails_release.py` 和发布工作流都按这个名字找。

版本号用 `-X main.version=` 注入，例如 `-ldflags "-s -w -X main.version=2.3.3"`；不注入时用源码里写的默认值。

## macOS ARM64 自动发布

在 macOS 上，推荐直接运行：

```sh
python3 scripts/build_release.py
```

也可以双击项目根目录的 `build-release.command`。脚本会运行测试、编译 ARM64 启动器、生成带时间戳的 `.app` 和 `.dmg`，不会覆盖旧版本。

如果需要构建后直接安装到应用程序目录：

```sh
python3 scripts/build_release.py --install
```

脚本需要一份已经构建好的 GUI 核心。默认优先使用 `/Applications/SteamCollectionImporter.app` 里的核心；也可以先按上一节从源码构建，再用路径指定：

```sh
python3 scripts/build_release.py --gui-core /path/to/SteamCollectionImporterCore
```

## Wails 系统 WebView 桌面版

`desktop-wails` 是新的桌面窗口外壳。它把现有 GUI 放入系统 WebView：macOS 使用 WKWebView，Windows 使用 WebView2，因此不会再自动打开外部浏览器。Wails 外壳启动随 App 携带的 `SteamCollectionImporterCore`，现有页面和 Steam 操作逻辑保持不变。

开发构建需要 Wails v2.10+：

```sh
cd desktop-wails
wails build
```

构建 App 时，把 `SteamCollectionImporterCore` 放到 App 的 `Resources` 目录；也可以用环境变量 `STEAM_COLLECTION_CORE` 指定核心路径。核心可以从本仓库源码构建（见上文），Windows 发行版需要对应架构的核心二进制；没有核心时程序会显示明确的启动错误，不会打开空白窗口。

macOS 本机可以直接双击根目录的 `build-wails.command`，或运行：

```sh
python3 scripts/build_wails_release.py --platform darwin/arm64 --install
```

## GitHub Actions 自动发布

`.github/workflows/release.yml` 会在推送 `v*` 标签时构建两个包（手动触发只试编译、不发布）：

| 平台 | 产物 |
|---|---|
| Windows x64 | `SteamCollectionImporter-windows-x64.zip` |
| macOS Apple Silicon | `SteamCollectionImporter-macOS-arm64.dmg` / `.zip` |

**不需要配置任何 Secret 或变量。** 图形核心就是 `work/importer-v2` 编译出来的，工作流在各自平台的 runner 上直接编译（`CGO_ENABLED=0`），再交给 `scripts/build_wails_release.py` 打进 App 的 `Resources/`。这样核心与外壳永远同源同版本，不会出现「外壳 2.3.2 + 核心 1.0.0」的错配。

外壳没有自己的界面代码，是把核心的网页用反向代理装进系统 WebView；核心与外壳之间的三条硬约定（Origin 改写、首页走 `/ui`、审核窗口用内嵌浮层替代 `window.open`）记在 [`ARCHITECTURE.md`](ARCHITECTURE.md) 里，`desktop-wails/main_test.go` 有对应单测。

版本号由工作流注入 `-ldflags "-X main.version=<tag去v>"`，所以发布出来的二进制里 `SteamCollectionImporterCore -manifest-info` 显示的版本与 Release 标签一致。

发版两种方式：

```sh
# 1) 推标签，最常用
git tag v2.3.3
git push origin v2.3.3
```

```text
# 2) 手动：Actions → 构建桌面版并发布 Release → Run workflow
#    输入版本号（如 v2.3.3），留空则用 v0.0.0-run.<运行号>
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

本项目可以直接交给 WorkBuddy 继续开发。建议先读 [`ARCHITECTURE.md`](ARCHITECTURE.md) 了解三层结构和读写数据流，再看 `desktop-wails/main.go`（外壳如何拉起核心并接管 WebView）、`scripts/build_wails_release.py`（打包脚本）和 `.github/workflows/release.yml`（发布流程）。

本仓库不包含个人 Steam 库、分类方案、价格缓存、历史备份或编译产物。

构建桌面版**不需要任何额外配置**：`release.yml` 在各自平台的 runner 上直接编译核心（`CGO_ENABLED=0`），再复制进 App 的 `Resources/`，所以不存在预先上传 `GUI_CORE_*_URL` 的步骤。发版只需推一个 `v*` 标签。

日常提交由 `.github/workflows/ci.yml` 把关：`go vet`、`go test`、桥接脚本测试、五平台交叉编译，以及一条防止个人数据入库的检查。
