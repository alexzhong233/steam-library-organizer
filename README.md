# Steam Library Organizer

A cross-platform Steam collection organizer for Windows and macOS. It reads a
Steam library and collection snapshot, lets you review a manual or AI-assisted
classification plan, previews the changes, backs up the current collection
state, and only writes after explicit confirmation.

The tool preserves dynamic collections and protects the current Steam state
with a preflight check. It supports OpenAI-compatible endpoints, custom model
settings, JSON import/export, history backups, price metadata, and a guarded
batch removal flow for eligible free games.

## Source

The importer is in [`work/importer-v2`](work/importer-v2). It includes the Go
backend, embedded Steam CEF bridge, tests, and vendored WebSocket dependency.
`mac_wrapper.go` contains the macOS launcher/theme proxy used by the ARM64 app.
Personal library snapshots, classification plans, prices, and built apps are
ignored by Git and must be supplied locally.

## Build and test

Requirements: Go 1.23 or newer. From `work/importer-v2`:

```sh
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor -o SteamCollectionImporter.exe .
GOOS=windows GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter-arm64.exe .
GOOS=darwin GOARCH=arm64 go build -mod=vendor -o SteamCollectionImporter .
```

The checked-in `classification.example.json` is a harmless five-entry fixture
used for offline tests. Do not commit a real Steam library export or API key.

## Safety notes

- Dynamic collections are read-only and are never silently created, renamed, or
  rewritten.
- A write operation requires an explicit confirmation and creates a history
  backup first.
- Free-game removal performs a fresh eligibility check before it can proceed.
- Steam is not contacted by the unit tests.

Licensed under the MIT License.
