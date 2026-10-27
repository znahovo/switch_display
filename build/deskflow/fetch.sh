#!/usr/bin/env bash
#
# 下载、校验并裁剪 Deskflow 运行时，供打包时内置进 Switch Display。
#
# 用法：
#   ./build/deskflow/fetch.sh darwin arm64
#   ./build/deskflow/fetch.sh windows amd64
#   DESKFLOW_VERSION=1.28.0 ./build/deskflow/fetch.sh   # 升级版本
#
# 产物（已裁剪，可直接拷进安装包）：
#   build/deskflow/darwin/arm64/Contents/{MacOS/deskflow-core,Frameworks,PlugIns,Resources}
#   build/deskflow/windows/amd64/{deskflow-core.exe,*.dll,plugins/,LICENSE}
#
# 设计要点（都是踩过的坑，改动前先读 docs/deskflow-integration.md §11）：
#
# 1. 仓库不跟踪二进制：产物目录写进 .gitignore，构建时现拉。
# 2. 校验用 Deskflow release 自带的 sums.txt，不把哈希硬编码在这里——
#    升级版本只需要改 DESKFLOW_VERSION 一处。
# 3. macOS 的 deskflow-core 依赖 bundle 内的 Contents/Frameworks（rpath 是
#    @loader_path/../Frameworks），裁剪时必须保留那套相对目录结构，
#    不能把二进制单独拷出来（否则运行时报
#    `Library not loaded: @rpath/QtWidgets.framework/...`）。
# 4. Windows 只留 deskflow-core.exe 会加载的 DLL 与 Qt 插件目录，把 GUI
#    （deskflow.exe）、translations/、docs/ 丢掉，约 45M → 38M。
# 5. 幂等：版本标记文件匹配时直接退出，避免每次构建都重新下载 30M。

set -euo pipefail

DESKFLOW_VERSION="${DESKFLOW_VERSION:-1.27.0}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd)"

GOOS="${1:-$(go env GOOS 2>/dev/null || echo unknown)}"
GOARCH="${2:-$(go env GOARCH 2>/dev/null || echo unknown)}"

OUT_DIR="$SCRIPT_DIR/$GOOS/$GOARCH"
CACHE_DIR="${DESKFLOW_CACHE_DIR:-$REPO_ROOT/.task/deskflow-cache}"
STAMP="$OUT_DIR/.version"

MOUNT_DIR=""
WORK_DIR=""

log() { printf '[deskflow] %s\n' "$*" >&2; }
die() {
	printf '[deskflow] 错误：%s\n' "$*" >&2
	exit 1
}

cleanup() {
	# dmg 挂载点必须在临时目录删除前卸载，否则 macOS 会留下悬空挂载。
	if [ -n "$MOUNT_DIR" ] && [ -d "$MOUNT_DIR" ]; then
		hdiutil detach "$MOUNT_DIR" -quiet >/dev/null 2>&1 || true
	fi
	if [ -n "$WORK_DIR" ] && [ -d "$WORK_DIR" ]; then
		rm -rf "$WORK_DIR"
	fi
}
trap cleanup EXIT

# 兼容 macOS（shasum）与 Linux/CI（sha256sum）。
sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{ print $1 }'
	else
		shasum -a 256 "$1" | awk '{ print $1 }'
	fi
}

release_asset() {
	case "$GOOS/$GOARCH" in
	darwin/arm64) echo "deskflow-$DESKFLOW_VERSION-macos-arm64.dmg" ;;
	darwin/amd64) echo "deskflow-$DESKFLOW_VERSION-macos-x86_64.dmg" ;;
	windows/amd64) echo "deskflow-$DESKFLOW_VERSION-win-x64-portable.zip" ;;
	windows/arm64) echo "deskflow-$DESKFLOW_VERSION-win-arm64-portable.zip" ;;
	*) die "不支持的平台：${GOOS}/${GOARCH}（只内置 macOS 与 Windows）" ;;
	esac
}

# trim_macos 从 dmg 里拷出能独立运行 deskflow-core 的最小集合。
#
# 保留清单是实测出来的（见文档 §11）：删掉 QtDBus 会让 QtGui 加载失败，
# 删掉 plugins/platforms 会报 `Could not find the Qt platform plugin "cocoa"`。
trim_macos() {
	local dmg="$1" stage="$2"
	MOUNT_DIR="$WORK_DIR/mnt"
	mkdir -p "$MOUNT_DIR"

	# dmg 带许可协议，非交互 attach 会直接 canceled；必须喂 y。
	printf 'y\ny\ny\n' | hdiutil attach -nobrowse -readonly -noverify \
		-mountpoint "$MOUNT_DIR" "$dmg" >/dev/null ||
		die "挂载 ${dmg} 失败"

	local app="$MOUNT_DIR/Deskflow.app"
	[ -d "$app" ] || die "dmg 里没有 Deskflow.app"

	mkdir -p "$stage/Contents/MacOS" "$stage/Contents/Frameworks" \
		"$stage/Contents/PlugIns" "$stage/Contents/Resources"

	cp "$app/Contents/MacOS/deskflow-core" "$stage/Contents/MacOS/"

	local fw src
	for fw in QtCore QtGui QtWidgets QtNetwork QtDBus; do
		src="$app/Contents/Frameworks/$fw.framework"
		[ -d "$src" ] || die "缺少 ${fw}.framework（Deskflow 版本可能变了）"
		cp -R "$src" "$stage/Contents/Frameworks/"
	done

	local plug
	for plug in platforms tls networkinformation; do
		src="$app/Contents/PlugIns/$plug"
		[ -d "$src" ] || die "缺少 Qt 插件目录 ${plug}"
		cp -R "$src" "$stage/Contents/PlugIns/"
	done

	cp "$app/Contents/Resources/qt.conf" "$stage/Contents/Resources/"
	# GPL-2.0 要求随二进制分发许可证全文。
	if [ -f "$app/Contents/Resources/LICENSE" ]; then
		cp "$app/Contents/Resources/LICENSE" "$stage/Contents/Resources/"
	fi

	# 自检：拷出来的东西必须真的能跑，否则错误会推迟到用户点「共享」时才暴露。
	"$stage/Contents/MacOS/deskflow-core" --version >/dev/null 2>&1 ||
		die "裁剪后的 deskflow-core 无法运行（Frameworks/PlugIns 不完整）"

	hdiutil detach "$MOUNT_DIR" -quiet >/dev/null 2>&1 || true
	MOUNT_DIR=""
}

# trim_windows 只保留 core 及其运行期依赖。
trim_windows() {
	local zip="$1" stage="$2"
	local unpack="$WORK_DIR/unpack"
	mkdir -p "$unpack"

	command -v unzip >/dev/null 2>&1 || die "需要 unzip 来解包 ${zip}"
	unzip -q "$zip" -d "$unpack"

	# zip 里可能还有一层同名目录，两种布局都要兼容。
	local src="$unpack"
	if [ ! -f "$src/deskflow-core.exe" ]; then
		# 用 -quit 而不是 `| head -n 1`：后者会让 find 吃 SIGPIPE，
		# 在 pipefail 下把整个脚本带崩。
		local found
		found="$(find "$unpack" -maxdepth 2 -name 'deskflow-core.exe' -print -quit)"
		[ -n "$found" ] || die "zip 里没有 deskflow-core.exe"
		src="$(dirname "$found")"
	fi
	[ -f "$src/deskflow-core.exe" ] || die "zip 里没有 deskflow-core.exe"

	mkdir -p "$stage"
	cp "$src/deskflow-core.exe" "$stage/"

	# 顶层的 Qt / OpenSSL / MSVC 运行库全部保留：逐个核依赖不划算，
	# 反正只有十几兆，漏一个 DLL 就是用户端起不来。
	local f
	for f in "$src"/*.dll; do
		[ -e "$f" ] || continue
		cp "$f" "$stage/"
	done

	[ -d "$src/plugins" ] || die "zip 里没有 plugins 目录"
	cp -R "$src/plugins" "$stage/"

	[ -f "$src/LICENSE" ] && cp "$src/LICENSE" "$stage/"
	[ -f "$src/LICENSE_EXCEPTION" ] && cp "$src/LICENSE_EXCEPTION" "$stage/"
	return 0
}

ASSET="$(release_asset)"

if [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$DESKFLOW_VERSION" ] && [ -d "$OUT_DIR" ]; then
	log "${GOOS}/${GOARCH} 已是 v${DESKFLOW_VERSION}，跳过下载（删掉 ${OUT_DIR} 可强制重拉）"
	exit 0
fi

BASE_URL="https://github.com/deskflow/deskflow/releases/download/v$DESKFLOW_VERSION"
mkdir -p "$CACHE_DIR"

# 1) 拉取校验清单。没有 sums.txt 就无法证明二进制未被篡改，直接失败。
SUMS="$CACHE_DIR/sums-$DESKFLOW_VERSION.txt"
if [ ! -s "$SUMS" ]; then
	log "下载校验清单 sums.txt（v${DESKFLOW_VERSION}）"
	curl -fsSL --retry 3 --retry-delay 2 -o "$SUMS" "$BASE_URL/sums.txt" ||
		die "下载 sums.txt 失败（检查网络，或确认 v${DESKFLOW_VERSION} 存在）"
fi

EXPECTED_SHA="$(awk -v want="$ASSET" '$2 == want { print $1 }' "$SUMS")"
[ -n "$EXPECTED_SHA" ] || die "sums.txt 里没有 ${ASSET}，版本号可能不对"

# 2) 下载资产（缓存命中则跳过）。
ARTIFACT="$CACHE_DIR/$ASSET"
if [ -f "$ARTIFACT" ] && [ "$(sha256_of "$ARTIFACT")" != "$EXPECTED_SHA" ]; then
	log "缓存里的 ${ASSET} 校验不符，重新下载"
	rm -f "$ARTIFACT"
fi
if [ ! -f "$ARTIFACT" ]; then
	log "下载 ${ASSET}"
	curl -fL --retry 3 --retry-delay 2 -o "$ARTIFACT.part" "$BASE_URL/$ASSET" ||
		die "下载 ${ASSET} 失败"
	mv "$ARTIFACT.part" "$ARTIFACT"
fi

ACTUAL_SHA="$(sha256_of "$ARTIFACT")"
[ "$ACTUAL_SHA" = "$EXPECTED_SHA" ] ||
	die "${ASSET} 校验失败：期望 ${EXPECTED_SHA}，实际 ${ACTUAL_SHA}"
log "校验通过（sha256 ${ACTUAL_SHA}）"

# 3) 裁剪并落到目标目录。
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/deskflow-fetch.XXXXXX")"
case "$GOOS" in
darwin) trim_macos "$ARTIFACT" "$WORK_DIR/stage" ;;
windows) trim_windows "$ARTIFACT" "$WORK_DIR/stage" ;;
*) die "不支持的平台：${GOOS}" ;;
esac

rm -rf "$OUT_DIR"
mkdir -p "$(dirname "$OUT_DIR")"
mv "$WORK_DIR/stage" "$OUT_DIR"
printf '%s' "$DESKFLOW_VERSION" >"$STAMP"

log "完成 -> ${OUT_DIR#"$REPO_ROOT"/}（$(du -sh "$OUT_DIR" | cut -f1)）"
