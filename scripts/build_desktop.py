#!/usr/bin/env python3
"""把 work/importer-v2 编译成「单文件双击运行」的桌面程序。

产物形态：
  Windows  SteamCollectionImporter.exe          —— 一个文件，没有 Resources 目录
  macOS    SteamCollectionImporter.app/Contents/MacOS/SteamCollectionImporter
                                               —— .app 里只有这一个可执行文件

和旧流程的区别：以前是「外壳 exe + Resources 里的 core exe」，靠解析 core 打印的
`GUI: http://127.0.0.1:PORT/` 再做反向代理，程序必然是双文件双进程。现在桌面入口
（-tags desktop）与核心同包，窗口直接调用内存里的 handler，没有任何本地端口。
"""

import argparse
import os
import plistlib
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "work/importer-v2"
APP_NAME = "SteamCollectionImporter"
BUNDLE_ID = "cn.alexzhong.steam-collection-organizer"

INFO_PLIST = {
    "CFBundleExecutable": APP_NAME,
    "CFBundleIdentifier": BUNDLE_ID,
    "CFBundleName": "Steam 收藏整理",
    "CFBundleDisplayName": "Steam 收藏整理",
    "CFBundlePackageType": "APPL",
    "CFBundleInfoDictionaryVersion": "6.0",
    "LSMinimumSystemVersion": "11.0",
    "NSHighResolutionCapable": True,
    "NSPrincipalClass": "NSApplication",
}


def run(cmd, **kwargs):
    printable = " ".join(str(c) for c in cmd)
    print("+", printable, flush=True)
    subprocess.run([str(c) for c in cmd], check=True, **kwargs)


def build(args) -> int:
    try:
        goos, goarch = args.platform.split("/", 1)
    except ValueError:
        print(f"--platform 需要 <os>/<arch>，收到 {args.platform!r}", file=sys.stderr)
        return 2
    if goos not in ("windows", "darwin"):
        print("只支持 windows 与 darwin", file=sys.stderr)
        return 2

    version = args.version.lstrip("v")
    outdir = Path(args.out).resolve()
    outdir.mkdir(parents=True, exist_ok=True)

    env = dict(os.environ)
    # WKWebView 通过 cgo 调 objc，macOS 必须开 cgo；Windows 用纯 Go 的 WebView2 绑定，关掉即可。
    env.update({
        "GOOS": goos,
        "GOARCH": goarch,
        "CGO_ENABLED": "1" if goos == "darwin" else "0",
        "GOFLAGS": "",
    })
    if goos == "darwin":
        # wails 的 WailsContext.m 在文件对话框里用 UTType 做扩展名过滤，但它的
        # #cgo LDFLAGS 只写了 Foundation/Cocoa/WebKit，没写 UniformTypeIdentifiers，
        # 链接期直接 "Undefined symbols: _OBJC_CLASS_$_UTType"。补在这里。
        #
        # 同时把最低系统版本钉在 11.0：UTType 是 11.0 才有的 API，而 clang 不指定
        # -mmacosx-version-min 时默认取构建机版本（runner 上是 14），产出的二进制
        # 会平白要求 macOS 14。11.0 也是 Apple Silicon 的下限。
        env["CGO_CFLAGS"] = "-mmacosx-version-min=11.0"
        env["CGO_LDFLAGS"] = "-mmacosx-version-min=11.0 -framework UniformTypeIdentifiers"
    ldflags = f"-s -w -X main.version={version}"
    if goos == "windows":
        ldflags += " -H=windowsgui"  # 双击不弹黑框
        binary = outdir / f"{APP_NAME}.exe"
    else:
        binary = outdir / APP_NAME

    run([shutil.which("go") or "go", "build",
         "-tags", "desktop,production", "-trimpath",
         "-ldflags", ldflags, "-o", binary, "."], cwd=CORE, env=env)
    if not binary.is_file():
        print(f"go build 声称成功，但没有生成 {binary}", file=sys.stderr)
        return 1
    print(f"可执行文件：{binary}（{binary.stat().st_size / 1048576:.1f} MB）", flush=True)

    if goos == "windows":
        return 0

    app = outdir / f"{APP_NAME}.app"
    if app.exists():
        shutil.rmtree(app)
    macos_dir = app / "Contents" / "MacOS"
    macos_dir.mkdir(parents=True)
    shutil.move(str(binary), macos_dir / APP_NAME)

    meta = dict(INFO_PLIST)
    meta["CFBundleShortVersionString"] = version
    meta["CFBundleVersion"] = version
    with (app / "Contents" / "Info.plist").open("wb") as fh:
        plistlib.dump(meta, fh)

    # 没有开发者证书也要签名，否则 Apple Silicon 上直接拒绝启动。
    # ad-hoc（"-"）不需要证书，代价是首次打开要用户手动放行。
    if shutil.which("codesign"):
        run(["codesign", "--force", "--deep", "--sign", "-", app])
    print(f"App：{app}", flush=True)

    if args.dmg and shutil.which("hdiutil"):
        dmg = outdir / f"{APP_NAME}-macOS-{goarch}.dmg"
        if dmg.exists():
            dmg.unlink()
        run(["hdiutil", "create", "-volname", "Steam 收藏整理",
             "-srcfolder", app, "-ov", "-format", "UDZO", dmg])
        print(f"DMG：{dmg}", flush=True)
    return 0


def main() -> int:
    # Windows 控制台默认 cp1252，打印中文会直接 UnicodeEncodeError，
    # 以前就是这一步把编译成功的 job 判成失败。
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8", errors="replace")
        except AttributeError:
            pass

    parser = argparse.ArgumentParser(description="编译单文件桌面版")
    parser.add_argument("--platform", default="windows/amd64",
                        choices=["windows/amd64", "darwin/arm64", "darwin/amd64"])
    parser.add_argument("--version", default="0.0.0", help="注入 main.version，通常传标签去 v")
    parser.add_argument("--out", default=str(ROOT / "dist"), help="产物目录")
    parser.add_argument("--dmg", action="store_true", help="macOS 额外生成 DMG")
    return build(parser.parse_args())


if __name__ == "__main__":
    sys.exit(main())
