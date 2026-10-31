#!/usr/bin/env python3
"""Build the current macOS Apple Silicon release.

The GUI core is kept as a local binary because the historical GUI source was
not part of this repository.  Pass --gui-core when building on another Mac;
without it the script uses the installed app or the local output bundle.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CORE_CANDIDATES = [
    Path("/Applications/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore"),
    ROOT / "outputs/SteamCollectionImporter/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore",
    Path.home() / "Applications/SteamCollectionImporter.app/Contents/Resources/SteamCollectionImporterCore",
]


def run(command: list[str], *, cwd: Path | None = None, env: dict[str, str] | None = None) -> None:
    print("+", " ".join(command))
    subprocess.run(command, cwd=cwd, env=env, check=True)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def find_core(explicit: Path | None) -> Path:
    candidates = [explicit] if explicit else []
    candidates.extend(CORE_CANDIDATES)
    for candidate in candidates:
        if candidate and candidate.is_file():
            return candidate.resolve()
    searched = "\n".join(f"  - {p}" for p in candidates if p)
    raise SystemExit(
        "找不到 GUI 核心程序。请把现有 GUI 核心路径传给 --gui-core，例如：\n"
        "python3 scripts/build_release.py --gui-core /path/to/SteamCollectionImporterCore\n"
        f"已搜索：\n{searched}"
    )


def write_plist(path: Path) -> None:
    path.write_text(
        """<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>SteamCollectionImporter</string>
<key>CFBundleIdentifier</key><string>local.steam-collection-organizer</string>
<key>CFBundleName</key><string>Steam 收藏整理</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>2.3.1</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
<key>NSHighResolutionCapable</key><true/>
<key>LSUIElement</key><false/>
</dict></plist>
""",
        encoding="utf-8",
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="构建 Steam 收藏整理工具 macOS ARM64 release")
    parser.add_argument("--gui-core", type=Path, help="当前 GUI 核心二进制路径")
    parser.add_argument("--output-dir", type=Path, help="release 输出父目录，默认 dist/")
    parser.add_argument("--install", action="store_true", help="构建后复制到 /Applications")
    parser.add_argument("--skip-tests", action="store_true", help="跳过 Go 测试")
    args = parser.parse_args()

    if sys.platform != "darwin":
        raise SystemExit("此脚本当前需要在 macOS 上运行；Windows release 请使用对应平台的 Go 构建环境。")
    if shutil.which("go") is None:
        raise SystemExit("未找到 Go。请先安装 Go 1.23 或更新版本。")
    if shutil.which("hdiutil") is None:
        raise SystemExit("未找到 hdiutil，无法生成 DMG。")

    core = find_core(args.gui_core)
    if not args.skip_tests:
        run(["go", "test", "-mod=vendor", "./..."], cwd=ROOT / "work/importer-v2")

    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    output_parent = (args.output_dir or ROOT / "dist").resolve()
    release_dir = output_parent / f"SteamCollectionImporter-macOS-arm64-{timestamp}"
    app = release_dir / "SteamCollectionImporter.app"
    contents = app / "Contents"
    macos = contents / "MacOS"
    resources = contents / "Resources"
    macos.mkdir(parents=True)
    resources.mkdir(parents=True)

    wrapper = macos / "SteamCollectionImporter"
    build_env = os.environ.copy()
    build_env.update({"GOOS": "darwin", "GOARCH": "arm64", "GO111MODULE": "off"})
    run(["go", "build", "-trimpath", "-o", str(wrapper), "mac_wrapper.go"], cwd=ROOT, env=build_env)
    shutil.copy2(core, resources / "SteamCollectionImporterCore")
    wrapper.chmod(0o755)
    (resources / "SteamCollectionImporterCore").chmod(0o755)
    write_plist(contents / "Info.plist")
    run(["codesign", "--force", "--deep", "--sign", "-", str(app)])

    readme = ROOT / "outputs/SteamCollectionImporter/README.md"
    if readme.is_file():
        shutil.copy2(readme, release_dir / "README.md")
    (release_dir / "配置说明.txt").write_text(
        "首次启动：双击 SteamCollectionImporter.app。\n"
        "设置和历史：~/.steam-collection-organizer/\n"
        "AI 配置：打开工具右侧“AI 整理”，填写 OpenAI 兼容地址、API Key 和模型；保存后会写入上述目录的 settings.json。\n"
        "系统拦截时：右键 SteamCollectionImporter.app，选择“打开”。\n",
        encoding="utf-8",
    )
    info = {
        "built_at": datetime.now().astimezone().isoformat(),
        "platform": "darwin/arm64",
        "wrapper_sha256": sha256(macos / "SteamCollectionImporter"),
        "gui_core_sha256": sha256(resources / "SteamCollectionImporterCore"),
        "gui_core_source": str(core),
        "tests": not args.skip_tests,
    }
    (release_dir / "BUILD-INFO.json").write_text(json.dumps(info, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    dmg = output_parent / f"SteamCollectionImporter-macOS-arm64-{timestamp}.dmg"
    run(["hdiutil", "create", "-volname", "SteamCollectionImporter", "-srcfolder", str(release_dir), "-ov", "-format", "UDZO", str(dmg)])

    if args.install:
        target = Path("/Applications/SteamCollectionImporter.app")
        if target.exists():
            shutil.rmtree(target)
        shutil.copytree(app, target)
        run(["codesign", "--force", "--deep", "--sign", "-", str(target)])
        print("已安装：", target)
    print("\nRelease 已生成：")
    print("  App:", app)
    print("  DMG:", dmg)
    print("  配置说明:", release_dir / "配置说明.txt")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
