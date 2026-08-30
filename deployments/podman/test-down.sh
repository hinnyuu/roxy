#!/usr/bin/env bash
# 拆除 test-up.sh 起的拓扑。--volumes 同时删除数据/配置卷（默认保留）。
set -euo pipefail

REMOVE_VOLUMES=false
for arg in "$@"; do
  case "$arg" in
    --volumes) REMOVE_VOLUMES=true ;;
    *) echo "未知参数: $arg" >&2; exit 2 ;;
  esac
done

for c in roxy roxy-jellyfin roxy-emby; do
  if podman ps -a --format '{{.Names}}' | grep -qx "$c"; then
    echo "移除容器 $c"
    podman rm -f "$c" >/dev/null
  fi
done

if [[ "$REMOVE_VOLUMES" == true ]]; then
  for v in roxy-data roxy-jf-config roxy-emby-config; do
    if podman volume exists "$v" 2>/dev/null; then
      echo "删除卷 $v"
      podman volume rm "$v" >/dev/null
    fi
  done
fi

echo "完成。"
