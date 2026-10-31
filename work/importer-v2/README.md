# Steam Collection Importer source

This directory contains the cross-platform Go importer and its Steam CEF bridge.
Personal Steam snapshots and classification plans are deliberately excluded from
version control. Supply a classification JSON through the GUI/import workflow
when using a real library.

Build and test from this directory:

```sh
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor -o SteamCollectionImporter.exe .
GOOS=windows GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter-arm64.exe .
GOOS=darwin GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter .
```
