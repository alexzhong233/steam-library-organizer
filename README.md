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

## GitHub Actions 自动发布三个版本

仓库自带 `.github/workflows/release.yml`。它会运行测试并构建：

- Windows x64：`SteamCollectionImporter-windows-x64.exe`
- Windows ARM64：`SteamCollectionImporter-windows-arm64.exe`
- macOS ARM64：签名的 `.app` 和 `.dmg`

Windows 两个版本使用仓库中的 Go 源码，可以直接构建。macOS 图形版还需要历史 GUI 核心二进制；该核心没有提交到仓库，也不应把个人 Steam 数据一起提交。由于核心文件约 8.7 MB，不能放进 GitHub Actions 的 Secret（Secret 有大小限制）。请把核心放到 GitHub Release 资产、对象存储或自己的静态下载地址，然后在仓库 **Settings → Secrets and variables → Actions → Variables** 新建：

- `GUI_CORE_URL`：核心文件的下载 URL。
- `GUI_CORE_SHA256`：可选的核心 SHA256，用于构建时校验。

本机核心的校验值可这样取得：

```sh
shasum -a 256 /Applications/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore
```

如果下载地址是私有的，再在 **Secrets** 中新建 `GUI_CORE_DOWNLOAD_TOKEN`；工作流会以 Bearer Token 下载。配置完成后，在 GitHub 的 **Actions → 构建并发布 Release → Run workflow** 输入版本号（例如 `v1.0.0`）；或者推送一个 `v*` 标签：

```sh
git tag v1.0.0
git push origin v1.0.0
```

工作流会在三个构建都成功后创建 GitHub Release，并附上两个 Windows 压缩包、macOS DMG、macOS tar.gz 和校验文件。若未设置 `GUI_CORE_URL`，macOS 作业会直接失败，不会发布缺少核心程序的不可用 App。

请注意：仓库目前公开的是 `work/importer-v2` 的 Go 导入器源码，Windows Release 构建的是这套源码；当前网页 GUI 核心没有纳入仓库。若要让 Windows Release 也使用另一套 GUI 核心，需要先把对应的 Windows GUI 源码或可下载构建产物接入工作流。

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
