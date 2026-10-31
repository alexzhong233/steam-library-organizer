# Steam 收藏整理 Wails 桌面壳

这个目录把现有 Steam 收藏整理 GUI 放进系统 WebView：

- macOS 使用系统 WKWebView；
- Windows 使用 WebView2；
- 不再自动打开 Chrome、Safari 或其他外部浏览器。

Wails 会把 Core 的本地 HTTP 页面反向代理到桌面窗口，因此 `/api`、审核窗口和历史页面仍使用同一个来源，不需要 iframe。

当前版本仍将 `SteamCollectionImporterCore` 作为后端 sidecar。将核心放在生成 App 的 `Resources/SteamCollectionImporterCore`，或设置 `STEAM_COLLECTION_CORE` 指向它。

开发环境需要 Go 1.23+、Wails v2.10+ 和对应平台的 WebView 开发环境。构建命令：

```sh
wails build
```

这里只负责桌面窗口和 WebView；页面业务、Steam 连接、收藏保存和历史记录继续由 Core 提供。
