# Steam 收藏整理工具源码

本目录包含跨平台 Go 导入器、Steam CEF 桥接、测试和 vendored WebSocket 依赖。

个人 Steam 库快照和个人分类方案不会进入版本控制。使用真实库时，请通过 GUI 的 JSON 导入或 AI 审核流程提供分类方案。

## 测试和编译

在本目录运行：

```sh
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor -o SteamCollectionImporter.exe .
GOOS=windows GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter-arm64.exe .
GOOS=darwin GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter .
```

真实 Steam 账号、收藏和许可证不会被单元测试访问或修改。
