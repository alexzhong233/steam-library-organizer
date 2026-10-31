# Steam 收藏整理工具

这是一个适用于 Windows 和 macOS 的 Steam 收藏整理工具。它可以读取 Steam 游戏库和收藏，导入手动或 AI 生成的分类方案，在独立审核窗口中检查修改，并在保存前自动备份当前收藏。

工具支持 OpenAI 兼容接口、自定义模型、JSON 导入导出、历史记录、商店价格缓存，以及带实时资格检查的免费游戏批量移除。动态收藏只读取，不会被程序静默创建、改名或改写。

## 源码

通用 Go 源码位于 [`work/importer-v2`](work/importer-v2)，包含 Steam CEF 桥接、测试和 vendored WebSocket 依赖。`mac_wrapper.go` 是 macOS ARM64 启动器和主题代理。

个人 Steam 库快照、个人分类方案、价格缓存和编译产物不会提交到 Git，需要在本机单独提供。

## 测试和手动编译

要求 Go 1.23 或更高版本。在 `work/importer-v2` 目录运行：

```sh
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor -o SteamCollectionImporter.exe .
GOOS=windows GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter-arm64.exe .
GOOS=darwin GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter .
```

仓库中的 `classification.example.json` 只是用于离线测试的五条示例数据。不要把真实 Steam 库导出文件或 API Key 提交到 Git。

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

当前 GUI 核心是本机已有的二进制文件。脚本会优先使用 `/Applications/SteamCollectionImporter.app` 中的核心；如果核心在其他位置，可以指定：

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

构建 App 时，把 `SteamCollectionImporterCore` 放到 App 的 `Resources` 目录；也可以用环境变量 `STEAM_COLLECTION_CORE` 指定核心路径。当前仓库只包含桌面外壳，Windows 发行版仍需要对应架构的 Core 二进制；没有 Core 时程序会显示明确的启动错误，不会打开空白窗口。

macOS 本机可以直接双击根目录的 `build-wails.command`，或运行：

```sh
python3 scripts/build_wails_release.py --platform darwin/arm64 --install
```

## GitHub Actions 自动发布三个 Wails 桌面版本

仓库的 `.github/workflows/release.yml` 会运行测试，分别构建 Wails 系统 WebView 版：

- Windows x64：`SteamCollectionImporter-windows-x64.zip`
- Windows ARM64：`SteamCollectionImporter-windows-arm64.zip`
- macOS ARM64：`SteamCollectionImporter-macOS-arm64.dmg` 和 `.zip`

Windows 和 macOS 的 GUI Core 是平台和架构相关的二进制文件，没有提交到 Git。由于 Core 文件约 8.7 MB，不能放进 GitHub Actions Secret。请把三个 Core 放到 GitHub Release 资产、对象存储或自己的静态下载地址，然后在仓库 **Settings → Secrets and variables → Actions → Variables** 配置：

- `GUI_CORE_WINDOWS_X64_URL`
- `GUI_CORE_WINDOWS_X64_SHA256`（可选）
- `GUI_CORE_WINDOWS_ARM64_URL`
- `GUI_CORE_WINDOWS_ARM64_SHA256`（可选）
- `GUI_CORE_MAC_ARM64_URL`
- `GUI_CORE_MAC_ARM64_SHA256`（可选）

本机 Core 的校验值可这样取得：

```sh
shasum -a 256 /Applications/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore
```

如果下载地址是私有的，再在 **Secrets** 中新建 `GUI_CORE_DOWNLOAD_TOKEN`；工作流会以 Bearer Token 下载。配置完成后，在 GitHub 的 **Actions → 构建桌面版并发布 Release → Run workflow** 输入版本号（例如 `v1.0.0`）；或者推送一个 `v*` 标签：

```sh
git tag v1.0.0
git push origin v1.0.0
```

三个构建都成功后，工作流会自动创建 GitHub Release，并附上两个 Windows 压缩包、macOS DMG、macOS ZIP 和 SHA256 校验文件。任一 Core 地址缺失或校验失败时，工作流会停止，不会发布缺少 GUI 的半成品。

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

本项目可以直接交给 WorkBuddy 继续开发。建议先阅读 `WORKBUDDY.md`，再查看 `desktop-wails/main.go`、`scripts/build_wails_release.py` 和 `.github/workflows/release.yml`。

本仓库不包含个人 Steam 库、分类方案、价格缓存、历史备份或 GUI Core 二进制。构建桌面版前，需要按 README 配置对应平台的 `GUI_CORE_*_URL` 和可选 SHA256。
