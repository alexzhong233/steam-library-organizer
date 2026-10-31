#!/usr/bin/env python3
"""Build the Wails desktop shell and attach a platform GUI core."""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
APP_NAME = "SteamCollectionImporter"


def run(args: list[str], cwd: Path) -> None:
    print("+", " ".join(args))
    subprocess.run(args, cwd=cwd, check=True)


def find_core(explicit: Path | None) -> Path:
    candidates = [explicit] if explicit else []
    candidates.extend(
        [
            Path("/Applications/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore"),
            ROOT / "outputs/SteamCollectionImporter/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore",
        ]
    )
    for candidate in candidates:
        if candidate and candidate.is_file():
            return candidate.resolve()
    raise SystemExit("找不到 GUI 核心，请用 --core 指定 SteamCollectionImporterCore 路径")


def main() -> int:
    # Windows runner 的 stdout 默认是 cp1252，打印中文（尤其全角冒号）会直接抛
    # UnicodeEncodeError。wails 明明编译成功了，脚本却在 print 上挂掉。
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")

    parser = argparse.ArgumentParser(description="构建 Wails 系统 WebView 版 Steam 收藏整理工具")
    parser.add_argument("--platform", choices=["darwin/arm64", "darwin/amd64", "windows/amd64", "windows/arm64"], default="darwin/arm64")
    parser.add_argument("--core", type=Path, help="对应平台的 SteamCollectionImporterCore")
    parser.add_argument("--install", action="store_true", help="macOS 构建后复制到 /Applications")
    args = parser.parse_args()

    wails = shutil.which("wails") or (Path.home() / "go/bin/wails")
    if not Path(wails).is_file() and shutil.which(str(wails)) is None:
        raise SystemExit("找不到 wails，请安装 Wails v2.10 或更新版本")
    core = find_core(args.core)
    project = ROOT / "desktop-wails"
    os_name, arch = args.platform.split("/", 1)
    # Windows 必须显式带 .exe：wails 把 -o 原样交给 go build，而 go build 只在
    # 没写 -o 时才自动补后缀，写 -o SteamCollectionImporter 出来就是无后缀文件。
    output_name = f"{APP_NAME}.exe" if os_name == "windows" else APP_NAME
    run([str(wails), "build", "-clean", "-platform", args.platform, "-o", output_name], project)

    output = project / "build/bin"
    if os_name == "darwin":
        app = output / f"{APP_NAME}.app"
        destination = app / "Contents/Resources/SteamCollectionImporterCore"
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(core, destination)
        destination.chmod(0o755)
        run(["codesign", "--force", "--deep", "--sign", "-", str(app)], ROOT)
        if args.install:
            target = Path("/Applications") / app.name
            if target.exists():
                shutil.rmtree(target)
            shutil.copytree(app, target)
            run(["codesign", "--force", "--deep", "--sign", "-", str(target)], ROOT)
            print("已安装：", target)
        print("App：", app)
    else:
        exe = output / output_name
        if not exe.is_file():
            bare = output / APP_NAME
            if bare.is_file():
                bare.replace(exe)
        resources = output / "Resources"
        resources.mkdir(parents=True, exist_ok=True)
        shutil.copy2(core, resources / "SteamCollectionImporterCore.exe")
        if not exe.is_file():
            found = sorted(p.name for p in output.iterdir()) if output.is_dir() else []
            raise SystemExit(f"wails 声称构建成功，但没有生成 {exe}；build/bin 里只有 {found}")
        print("EXE：", exe)
    print("核心：", core)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
