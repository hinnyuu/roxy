#!/usr/bin/env bash
# M2+ 宿主机测试拓扑一键起（podman CLI，见 docs/testing/README.md）。
# 用法：
#   deployments/podman/test-up.sh [--image REF] [--port 8080] [--selinux] [--with-jellyfin] [--with-emby]
# 默认仅起 roxy；Jellyfin/Emby 旗标为 M4 端到端测试预留。
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
SHARE="$REPO/test_share"

IMAGE="ghcr.io/hinnyuu/roxy:dev"
PORT=8080
MOUNT_OPTS="rw"
JF=false
EMBY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image) IMAGE="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --selinux) MOUNT_OPTS="rw,z"; shift ;;
    --with-jellyfin) JF=true; shift ;;
    --with-emby) EMBY=true; shift ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

mkdir -p "$SHARE/downloads" "$SHARE/library"

run() {
  local name="$1"; shift
  if podman ps -a --format '{{.Names}}' | grep -qx "$name"; then
    echo "容器 $name 已存在：先执行 test-down.sh" >&2
    exit 1
  fi
  podman run -d --name "$name" "$@"
}

echo "启动 roxy（镜像 $IMAGE，端口 127.0.0.1:$PORT）…"
run roxy \
  -p "127.0.0.1:$PORT:8080" \
  -v "$SHARE:/media:$MOUNT_OPTS" \
  -v roxy-data:/data \
  "$IMAGE"

SEL=""
[[ "$MOUNT_OPTS" == *,z ]] && SEL=",z"

if [[ "$JF" == true ]]; then
  echo "启动 Jellyfin（只读挂载，库禁刮削——见 docs/testing/m1.md §4.2）…"
  podman volume create roxy-jf-config >/dev/null
  run roxy-jellyfin \
    -p 127.0.0.1:8096:8096 \
    -v roxy-jf-config:/config \
    -v "$SHARE:/media:ro$SEL" \
    docker.io/jellyfin/jellyfin:10.11.11
fi

if [[ "$EMBY" == true ]]; then
  echo "启动 Emby（镜像需本地已有 4.9.5，按你环境调整）…"
  podman volume create roxy-emby-config >/dev/null
  run roxy-emby \
    -p 127.0.0.1:8097:8096 \
    -v roxy-emby-config:/config \
    -v "$SHARE:/media:ro$SEL" \
    emby/embyserver:latest
fi

echo
echo "roxy UI: http://127.0.0.1:$PORT （初始凭据 admin/admin，登录后请立即修改）"
echo "源目录（容器内路径）：/media/downloads → 宿主机 $SHARE/downloads"
