#!/bin/zsh
set -e
cd "$(dirname "$0")"
python3 scripts/build_wails_release.py --install
echo
echo "Wails 桌面版构建完成，按回车关闭窗口。"
read
