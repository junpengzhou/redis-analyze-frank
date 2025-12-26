#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "脚本目录: $SCRIPT_DIR"
echo "项目根目录: $PROJECT_ROOT"

cd "$PROJECT_ROOT"

echo "清理构建目录..."
rm -rf ./build/*
mkdir -p ./build

echo "开始构建..."
APP_NAME="frank"
go build -o "./build/$APP_NAME" ./cmd

chmod a+x "./build/$APP_NAME"

echo "构建完成: ./build/$APP_NAME"
echo "文件大小: $(du -h ./build/$APP_NAME | cut -f1)"