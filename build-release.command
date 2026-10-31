#!/bin/zsh
set -e
cd "$(dirname "$0")"
python3 scripts/build_release.py
echo
echo "Release 构建完成，按回车关闭窗口。"
read
