# 测试分工与检查单规范

## 分工原则

| 环境 | 执行者 | 范围 |
|---|---|---|
| 开发容器 | agent | 单元测试、golden 测试（解析 fixture、NFO 黄金样本、软链接布局模拟）、API 测试、`nix build` 冒烟 |
| 宿主机（podman） | 用户 | 真实媒体服务器集成测试、兼容性实测、端到端演练 |

开发容器**无嵌套容器能力**，跑不了 podman/docker/Jellyfin/Emby——凡涉及真实
服务器行为的结论，一律标注"待 M1 实测"（见 `docs/RESEARCH.md` §7），不得当成
已验证事实写进代码断言。

## 交付双轨

- **二进制直跑**：`nix build` 产物（Go 静态二进制）直接在宿主机运行，
  用于快速迭代调试。
- **OCI 镜像**：`ghcr.io/hinnyuu/roxy:dev`（main 分支）与版本标签，
  用于集成测试与长期部署。

## 宿主机测试拓扑（podman CLI 阶段）

```
所有容器同结构挂载 test_share → /media：
  roxy:      -v test_share:/media:rw  + DATA_DIR 卷 + -p 127.0.0.1:8080:8080
  jellyfin:  -v test_share:/media:ro  + 独立配置卷
  emby:      -v test_share:/media:ro  + 独立配置卷
  (M6) qbit: -v test_share:/media:rw
SELinux 环境追加 :z/:Z 挂载选项。
```

`deployments/podman/test-up.sh` / `test-down.sh`（M2 交付）提供一键起/拆。

## 检查单编写规范（每个里程碑一份，如 `m1.md`）

1. **前置条件**：镜像版本、配置要求、测试数据准备步骤。
2. **操作步骤**：可直接复制的命令序列。
3. **预期结果表**：`# | 操作 | 预期 | 实际 | 通过?` 逐行回填。
4. **失败反馈包**：日志收集命令（`podman logs`、roxy `data/` 中的日志、
   服务器扫描日志路径）、复现最小样本。

## 测试数据红线

- `test_share/` 内的真实媒体文件**永不入库**（.gitignore 已覆盖，
  禁止 `git add -f`）。
- 可入库的测试资产仅限 `testdata/`：纯文本/KB 级哑文件（解析 fixture、
  NFO 黄金样本、命名样本）。
- 测试中产生的任何密钥（API key 等）只放环境变量，不进检查单与日志。

## 各里程碑检查单索引

| 里程碑 | 检查单 | 状态 |
|---|---|---|
| M1 | `m1.md` 兼容性实测（命名规范冻结实验） | **完成**（2026-08-30，结果见文件 §6） |
| M1.5 | `m1.5.md` Jellyfin 多版本合并探针（D-038） | 待执行 |
| M2 | `m2.md` 扫描与规则解析 | 待编写 |
| M3 | `m3.md` LLM 匹配质量 | 待编写 |
| M4 | `m4.md` 端到端与漂移演练 | 待编写 |
| M5 | `m5.md` 返工演练场景 | 待编写 |
| M6 | `m6.md` 下载客户端集成与常驻部署 | 待编写 |
