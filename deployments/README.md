# deployments/

部署产物目录。

- `podman/`：开发测试用一键起/拆脚本（`test-up.sh` / `test-down.sh`），M2 交付。
- `quadlet/`：podman 常驻部署模板，M6 交付。
- `compose/`：docker compose 模板，M6 交付。

拓扑与挂载约定见 `docs/ARCHITECTURE.md` §2 与 `docs/testing/README.md`。
生产 OCI 镜像由 flake.nix（dockerTools）构建、GitHub Actions 推送至
`ghcr.io/hinnyuu/roxy`——**不维护 Dockerfile**（docs/DECISIONS.md D-030）。
