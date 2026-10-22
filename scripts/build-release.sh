#!/usr/bin/env bash
# 编译所有发布平台并打包，生成一份 sha256sums.txt。Release 工作流和 CI 都调用它，本地也可以直接运行：
#
#   scripts/build-release.sh v1.5.0 dist
#
# 发布平台在这里和 ci.yml 的 cross-build matrix 中各有一份，增删平台时两处同步修改。
set -euo pipefail

version=${1:?usage: $0 <version> [outdir]}
out=${2:-dist}
name=ipv6proxypool

# 系统/架构[/变体]：arm 的变体是 GOARM，mips 系列的变体是 GOMIPS
platforms=(
  linux/amd64
  linux/arm64
  linux/arm/7             # 树莓派、ARMv7 路由器
  linux/mipsle/softfloat  # MT7621 等 MIPS 路由器（OpenWrt）
  windows/amd64
  windows/arm64
)

cd "$(dirname "$0")/.."
rm -rf "$out"
mkdir -p "$out"
out=$(cd "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

for p in "${platforms[@]}"; do
  IFS=/ read -r goos goarch variant <<<"$p"
  goarm= gomips= arch=$goarch
  case $goarch in
    arm) goarm=$variant arch=armv$variant ;;
    mips*) gomips=$variant arch=$goarch${variant:+-$variant} ;;
  esac
  ext=
  if [ "$goos" = windows ]; then ext=.exe; fi
  pkg=$name-$version-$goos-$arch

  rm -rf "${stage:?}"/*
  # 静态编译（不依赖 glibc），去掉本地路径和调试符号，注入版本号
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch GOARM=$goarm GOMIPS=$gomips \
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$stage/$name$ext" .
  cp README.md config.ini hosts "$stage/"

  # 文件放在压缩包根目录，与之前的版本一致
  if [ "$goos" = windows ]; then
    (cd "$stage" && zip -q -X "$out/$pkg.zip" ./*)
  else
    # 明确列出文件，不打包 "." 目录项：否则解压时会把当前目录权限改成临时目录的 0700
    tar -C "$stage" --owner=0 --group=0 --numeric-owner -czf "$out/$pkg.tar.gz" "$name$ext" README.md config.ini hosts
  fi
  echo "built $pkg"
done

# 一份校验文件覆盖所有压缩包，下载后用 sha256sum -c sha256sums.txt --ignore-missing 校验
(cd "$out" && sha256sum -- * >sha256sums.txt)
echo "checksums: $out/sha256sums.txt"
