#!/usr/bin/env bash
#
# build.sh — astro-mountain 多平台发布构建脚本
#
# 全流程：同步内置默认 → 校验 configs/ 与内置默认一致 → 交叉编译
#         windows/darwin/linux × amd64/arm64 → 收拢 dist/ → 压缩 → 回读校验。
# （Windows 给 .zip、macOS 给 .tar.gz：zip 原生可解但丢 Unix 权限，
#  tar.gz 保权限、解压即可执行。）
#
# 用法：
#   ./build.sh                   # 完整发布构建（校验+编译+打包+压缩+回读）
#   VERSION=v1.2.3 ./build.sh    # 显式指定版本号
#   ./build.sh check-config      # 仅校验 configs/ 与内置默认是否分叉
#   ./build.sh clean             # 删除 dist/
#
set -euo pipefail

# ── 定位仓库根（不论从哪里调用脚本）──────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

MODULE_ROOT="$SCRIPT_DIR"
BIN_NAME="astro-mountain"
CMD_PKG="./cmd/astro-mountain"
DEFAULTS_DIR="internal/config/defaults"
DIST_DIR="$MODULE_ROOT/dist"

# ── 版本号 ──────────────────────────────────────────────────────
# 优先级：环境变量 VERSION > git 最近 tag > "dev"
if [[ -n "${VERSION:-}" ]]; then
  APP_VERSION="$VERSION"
elif git describe --tags --always --dirty 2>/dev/null | grep -q .; then
  APP_VERSION="$(git describe --tags --always --dirty 2>/dev/null)"
else
  APP_VERSION="dev"
fi

# 版本号会进文件名，先把路径不友好的字符换成 '-'，否则 tag 里带 '/' 会造出
# 一个意料之外的子目录。
SAFE_VERSION="$(printf '%s' "$APP_VERSION" | tr '/ :' '-')"

LDFLAGS="-s -w -X main.Version=${APP_VERSION}"

# 构建时间注入：PRD P0-20 要求 --version 同时输出版本号与构建时间。
# 但 -X 指向不存在的符号时 Go 链接器会「静默忽略」，那正是文档腐烂的温床——
# 所以这里显式探测 main.BuildTime 是否真的存在，不存在就明说没注入。
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if grep -qE '^[[:space:]]*(var[[:space:]]+)?BuildTime[[:space:]]*=' cmd/astro-mountain/main.go 2>/dev/null; then
  LDFLAGS="${LDFLAGS} -X main.BuildTime=${BUILD_TIME}"
  BUILD_TIME_INJECTED=1
else
  BUILD_TIME_INJECTED=0
fi

# ── verify_config：configs/ 与内置默认一致闸门（保留，不许删）──────
# 这是最后一道防线：CI 可跑 `./build.sh check-config`、或有人绕过 build.sh 直接
# go build 时，仍能拦下 configs/ 与 internal/config/defaults/ 的漂移。
verify_config() {
  local src_name="$1"
  local dst_name="${2:-$src_name}"
  local dist="configs/${src_name}"
  local embed="${DEFAULTS_DIR}/${dst_name}"
  if [[ ! -f "$dist" ]]; then
    echo "    [错误] 仓库根缺少 ${dist}" >&2
    return 1
  fi
  if [[ ! -f "$embed" ]]; then
    echo "    [错误] 内置默认缺失 ${embed}" >&2
    return 1
  fi
  # 规范化比较（忽略尾随换行/空白差异），报告首个差异行号
  # 不用 <(...) 进程替换，避免 POSIX sh（macOS /bin/sh 实为 bash POSIX 模式）
  # 解析失败。用临时文件语义等价且更易移植。
  local tmp_dist tmp_embed rc=0
  tmp_dist="$(mktemp)"
  tmp_embed="$(mktemp)"
  sed 's/[[:space:]]*$//' "$dist" > "$tmp_dist"
  sed 's/[[:space:]]*$//' "$embed" > "$tmp_embed"
  if ! diff "$tmp_dist" "$tmp_embed" >/dev/null; then
    echo "    [错误] ${src_name} 不一致：configs/ 与内置默认分叉！" >&2
    echo "    修复：本仓库已改为「configs/ 单一真相源 + defaults 派生」。" >&2
    echo "          运行 ./build.sh 会在编译前自动从 configs/ 重新生成内置默认；" >&2
    echo "          或直接执行：cp configs/config.example.json internal/config/defaults/config.json && cp configs/sites.json internal/config/defaults/sites.json" >&2
    diff "$dist" "$embed" >&2 || true
    rc=1
  fi
  rm -f "$tmp_dist" "$tmp_embed"
  if [ "$rc" -ne 0 ]; then
    return 1
  fi
  echo "    [ok] ${src_name}"
}

# ── clean 子命令 ───────────────────────────────────────────────
if [[ "${1:-}" == "clean" ]]; then
  echo "==> 清理 dist/ ..."
  rm -rf "$DIST_DIR"
  echo "==> 已删除 $DIST_DIR"
  exit 0
fi

# ── check-config 子命令（仅校验，不同步、不编译）────────────────
# 供 CI / 本地快速检测 configs/ 与内置默认是否分叉；分叉时直接失败并打印修复命令。
if [[ "${1:-}" == "check-config" ]]; then
  echo "==> 仅校验 configs/ 与内置默认一致（不自动同步）..."
  verify_config config.example.json config.json
  verify_config sites.json
  echo "==> configs/ 校验通过（无分叉）"
  exit 0
fi

echo "==> 版本号: ${APP_VERSION}"
if [[ "$BUILD_TIME_INJECTED" == "1" ]]; then
  echo "==> 构建时间: ${BUILD_TIME}（已注入 main.BuildTime）"
else
  echo "==> 构建时间: ${BUILD_TIME}（未注入：cmd/astro-mountain/main.go 里没有 BuildTime 变量）"
fi

# ── 0. 依赖的外部命令 ───────────────────────────────────────────
require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "    [错误] 缺少命令 '$1'：$2" >&2
    exit 1
  fi
}
require_cmd go "请先安装 Go 1.22+"
require_cmd zip "打 Windows 发布包需要它（macOS/Linux 一般自带）"
require_cmd unzip "回读校验 zip 包内容需要它"
require_cmd tar "打 macOS 发布包需要它"

# sha256 工具在 macOS 与 Linux 上名字不同
if command -v shasum >/dev/null 2>&1; then
  SHA_CMD=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD=(sha256sum)
else
  SHA_CMD=()
fi

# ── 0.5 同步内置默认（预防分叉，加在闸门之前）─────────────────
# configs/ 是唯一真相源；internal/config/defaults/ 是派生物——go:embed 只能引用
# 包目录内的文件，所以内置默认必须物理落在 internal/config/defaults/，不能反向
# embed 仓库根的 configs/。此前这俩是两份需人手同步的文件，冷湖镇就曾漏同步
# （被下方闸门拦下）。现在改为：编译前先由 tools/sync_config.sh 从 configs/ 重新
# 生成 defaults，分叉在源头被预防——下次改机位哪怕忘了同步，build.sh 也会自动
# 补齐，不会再次分叉后撞红构建。
echo "==> 同步内置默认（configs/ -> internal/config/defaults/）..."
sync_config() {
  if [[ -f "$SCRIPT_DIR/tools/sync_config.sh" ]]; then
    bash "$SCRIPT_DIR/tools/sync_config.sh"
  else
    # 兜底：脚本缺失时直接拷，避免构建整体失败。
    # 必须先 mkdir -p：defaults/*.json 已被 .gitignore 排除（configs/ 才是唯一真相源，
    # defaults/ 是派生物），全新 clone 后这个目录本身不存在，裸 cp 会因父目录缺失而失败。
    # 开源仓只用无密钥的 config.example.json 生成 embed（config.json 含密钥、已 gitignore）
    mkdir -p "$DEFAULTS_DIR"
    cp configs/config.example.json "$DEFAULTS_DIR/config.json"
    cp configs/sites.json "$DEFAULTS_DIR/sites.json"
    echo "    [sync] 兜底：直接 cp 覆盖 $DEFAULTS_DIR/"
  fi
}
sync_config

# ── 1. 校验 configs/ 与 embed 默认一致 ──────────────────────────
# 闸门保留（verify_config 定义见上方）：同步刚跑过，正常情况下这里一定通过；
# 它仍是最后一道防线——CI 跑 `./build.sh check-config`、或有人绕过 build.sh 直接
# go build 时，仍能拦下 configs/ 与内置默认的漂移。
echo "==> 校验 configs/ 与内置 embed 默认一致 ..."
verify_config config.example.json config.json
verify_config sites.json
echo "==> configs/ 校验通过"

# ── 2&3&4. 交叉编译 + 收拢产物目录 ──────────────────────────────
build_target() {
  local goos="$1"
  local goarch="$2"
  local out_dir="$DIST_DIR/${BIN_NAME}-${goos}-${goarch}"
  local out_bin="${out_dir}/${BIN_NAME}"
  if [[ "$goos" == "windows" ]]; then
    out_bin="${out_bin}.exe"
  fi

  echo "==> 构建 ${goos}/${goarch} ..."
  # 先清掉旧目录：残留的上一版文件会被一起打进压缩包。
  rm -rf "$out_dir"
  mkdir -p "$out_dir"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out_bin" "$CMD_PKG"
  chmod 755 "$out_bin"
  echo "    生成: $out_bin"

  # 附带 configs/ 与 README.md
  # 注意：configs/config.json 含真实 API 密钥且已被 gitignore，绝不能打进发布包——
  # 二进制内置默认由 config.example.json 生成（无密钥），缺失 config.json 时自动回退，
  # 故发布包只带 config.example.json + sites.json。下面显式删除密钥文件，双保险。
  cp -R configs "$out_dir/configs"
  rm -f "$out_dir/configs/config.json"
  if [[ -f "README.md" ]]; then
    cp README.md "$out_dir/README.md"
    echo "    附带: README.md"
  else
    echo "    [错误] 仓库根无 README.md，发布包必须带说明文档" >&2
    return 1
  fi
  # macOS 逛过的目录容易掉 .DS_Store，别让它进发布包
  find "$out_dir" -name '.DS_Store' -delete 2>/dev/null || true
  echo "    产物目录: $out_dir"
}

# ── 5. 压缩 ─────────────────────────────────────────────────────
# 归档内的顶层目录仍是 astro-mountain-<goos>-<goarch>/，与未压缩目录同名，
# 这样 README 里「解压后 cd astro-mountain-darwin-arm64」才成立。
archive_target() {
  local goos="$1"
  local goarch="$2"
  local dir_name="${BIN_NAME}-${goos}-${goarch}"
  local base="${BIN_NAME}_${SAFE_VERSION}_${goos}_${goarch}"
  local archive

  if [[ "$goos" == "windows" ]]; then
    archive="${DIST_DIR}/${base}.zip"
    rm -f "$archive"
    # -r 递归 / -q 安静 / -X 不写 macOS 扩展属性（免得 Windows 端多出垃圾字段）
    ( cd "$DIST_DIR" && zip -q -r -X "$(basename "$archive")" "$dir_name" -x '*.DS_Store' )
  else
    archive="${DIST_DIR}/${base}.tar.gz"
    rm -f "$archive"
    # COPYFILE_DISABLE=1：阻止 macOS bsdtar 塞入 ._xxx AppleDouble 伴生文件
    COPYFILE_DISABLE=1 tar --exclude '.DS_Store' \
      -czf "$archive" -C "$DIST_DIR" "$dir_name"
  fi

  echo "    压缩包: $archive"
  ARCHIVES+=("$archive")
}

# ── 6. 回读校验：压缩包里必须有那四样东西 ───────────────────────
verify_archive() {
  local archive="$1"
  local goos="$2"
  local goarch="$3"
  local dir_name="${BIN_NAME}-${goos}-${goarch}"
  local bin_entry="${dir_name}/${BIN_NAME}"
  [[ "$goos" == "windows" ]] && bin_entry="${bin_entry}.exe"

  local listing
  if [[ "$archive" == *.zip ]]; then
    listing="$(unzip -Z1 "$archive")"
  else
    listing="$(tar -tzf "$archive")"
  fi

  local missing=0
  local want
  for want in "$bin_entry" \
              "${dir_name}/configs/config.example.json" \
              "${dir_name}/configs/sites.json" \
              "${dir_name}/README.md"; do
    # tar 列目录时可能带尾随斜杠，用精确整行匹配
    if ! printf '%s\n' "$listing" | grep -qxF "$want"; then
      echo "    [错误] $(basename "$archive") 里缺少 ${want}" >&2
      missing=1
    fi
  done
  # 顺手拦截 macOS 伴生垃圾
  if printf '%s\n' "$listing" | grep -qE '(^|/)(\._|\.DS_Store)'; then
    echo "    [错误] $(basename "$archive") 混入了 .DS_Store / ._ 伴生文件" >&2
    missing=1
  fi
  # 安全闸门：发布包严禁含 configs/config.json（含真实 API 密钥，已被 gitignore）。
  # 一旦误打包，立刻让构建失败，杜绝密钥随 Release 外泄。
  if printf '%s\n' "$listing" | grep -qxF "${dir_name}/configs/config.json"; then
    echo "    [错误] $(basename "$archive") 混入了 configs/config.json（含 API 密钥，严禁发布）" >&2
    missing=1
  fi
  if [[ "$missing" == "1" ]]; then
    return 1
  fi

  local count
  count="$(printf '%s\n' "$listing" | grep -c . || true)"
  echo "    [ok] $(basename "$archive")  条目 ${count} 个，四件套齐全"
}

ARCHIVES=()

# 先清掉历史版本的归档。留着它们最危险的地方在于：结尾的产物清单只列本次构建的
# 包，旧包却静静躺在 dist/ 里，发版时很容易抓错文件。要保留旧版请自己先挪走。
if compgen -G "${DIST_DIR}/${BIN_NAME}_*.zip" >/dev/null 2>&1 ||
   compgen -G "${DIST_DIR}/${BIN_NAME}_*.tar.gz" >/dev/null 2>&1; then
  echo "==> 清理 dist/ 下的历史归档 ..."
  for old in "${DIST_DIR}/${BIN_NAME}"_*.zip "${DIST_DIR}/${BIN_NAME}"_*.tar.gz; do
    [[ -f "$old" ]] && echo "    移除: $(basename "$old")" && rm -f "$old"
  done
fi

build_target windows amd64
build_target darwin arm64
build_target linux amd64
build_target linux arm64

echo "==> 压缩发布包 ..."
archive_target windows amd64
archive_target darwin arm64
archive_target linux amd64
archive_target linux arm64

echo "==> 回读校验压缩包内容 ..."
verify_archive "${DIST_DIR}/${BIN_NAME}_${SAFE_VERSION}_windows_amd64.zip" windows amd64
verify_archive "${DIST_DIR}/${BIN_NAME}_${SAFE_VERSION}_darwin_arm64.tar.gz" darwin arm64
verify_archive "${DIST_DIR}/${BIN_NAME}_${SAFE_VERSION}_linux_amd64.tar.gz" linux amd64
verify_archive "${DIST_DIR}/${BIN_NAME}_${SAFE_VERSION}_linux_arm64.tar.gz" linux arm64

# ── 7. 汇总 ─────────────────────────────────────────────────────
echo ""
echo "==> 全部构建完成，版本 ${APP_VERSION}"
echo ""
echo "未压缩目录（本地调试用）："
for d in "$DIST_DIR"/"${BIN_NAME}"-*/; do
  [[ -d "$d" ]] && echo "  $d"
done
echo ""
echo "发布压缩包："
for a in "${ARCHIVES[@]}"; do
  size="$(du -h "$a" | cut -f1 | tr -d ' ')"
  if [[ ${#SHA_CMD[@]} -gt 0 ]]; then
    sum="$("${SHA_CMD[@]}" "$a" | awk '{print $1}')"
    echo "  $(basename "$a")  ${size}  sha256:${sum}"
  else
    echo "  $(basename "$a")  ${size}"
  fi
done
