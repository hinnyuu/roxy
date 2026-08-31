# AGENTS.md — roxy 项目级工作规范

本文件面向在 roxy 仓库中工作的 AI agent（与人类协作者）。全局环境规则见
`~/.config/opencode/AGENTS.md`（Nix 一律化、不可变产物、危险操作禁令等），本文件只记录
**项目特有**的约定。项目背景与全部设计决策见 `README.md`、`docs/ARCHITECTURE.md`、
`docs/DECISIONS.md`——**动手改代码前先读这三份文档**。

---

## 新会话快速开始

1. 先看 `docs/ROADMAP.md` 顶部的**"当前进度"**节（项目状态单一事实源），
   确认当前里程碑与下一步动作。
2. `docs/ARCHITECTURE.md` 较长（700+ 行），小任务按目录**按需读相关章节**；
   涉及设计决策变更时须通读 `docs/DECISIONS.md` 相关条目。
3. 写代码前确认所在里程碑的前置已完成（见 ROADMAP"当前进度"节的前置检查）。

---

## 项目一句话

roxy 是面向 Jellyfin/Emby/Kodi 的动漫媒体库整理器：LLM 智能识别 + NFO 元数据 +
软链接零破坏整理，BT 做种安全。

## 技术栈

- 后端：Go（静态单二进制），SQLite（状态与台账），fsnotify（文件监控，v1.x 起）
- 前端：React + TypeScript + Vite，构建产物经 `go:embed` 内嵌进二进制
- 构建/部署：flake.nix 三输出（devShell / packages.default / dockerTools OCI 镜像），
  GitHub Actions + ghcr.io 分发（**唯一构建定义是 flake.nix，禁止另写 Dockerfile**）
- 测试环境：宿主机 podman（开发容器内无嵌套，跑不了 podman/docker）

## 常用命令

> 以下命令自 M0 完成后生效（当前可用）。

```bash
nix develop                    # 进入开发环境（一切开发操作的入口）
go build ./...                 # 编译检查
go test ./...                  # 单元测试 + golden 测试
go vet ./... && gofmt -l cmd internal   # 静态检查（gofmt 输出必须为空；不检查 vendor/）
nix build                      # 生产二进制冒烟测试：./result/bin/roxy --help
nix build .#image              # OCI 镜像（dockerTools，无需 daemon）

# 前端（M2 起生效；本地 go build 默认走占位页，无需 npm）
npm --prefix web ci
npm --prefix web run build
npm --prefix web run lint
npm --prefix web run dev       # Vite dev server（代理 /api → 127.0.0.1:8080，需先起 roxy serve）

# 本地构建内嵌真实 UI 的二进制（生产一律 nix build，此仅开发调试用）
npm --prefix web run build && go build -tags=web -o bin/roxy ./cmd/roxy
```

若上述命令随项目演进发生变化，**必须同步更新本文件**。

## 硬性规则（违反即破坏项目根基）

1. **`test_share/` 与 `data/` 永不入库**：前者是宿主机测试用的真实媒体目录，后者是
   运行时状态。二者已在 `.gitignore`，任何情况下不得 `git add -f`。
2. **机密零落盘**：LLM API key、TMDB key、Bangumi token 等一律走环境变量
   （`ROXY_*` 前缀），禁止写入配置文件默认值、测试夹具或日志。日志中必须脱敏。
3. **零破坏原则**：roxy 对源文件（下载目录、字幕目录）永远只读。一切产出物只有
   软链接 + 小文件（NFO/图片），且必须登记进台账（ledger 表）。任何新产出路径都必须
   可经台账精确回滚。
4. **软链接相对路径优先**：链接目标默认用相对路径（依赖"单一公共父目录"部署形态）；
   绝对路径 + 路径映射只是兜底代码路径。
5. **命名规范已冻结，变更须重测**：文件系统层命名（目录/链接名）遵循
   `docs/ARCHITECTURE.md` §4（M1/M1.5 实测冻结，D-036/D-038/D-039）。任何
   命名变更必须先修 `docs/DECISIONS.md`，并重新列入实测清单重测后才可生效。
6. **LLM 输出无证据即无效**：所有 LLM 结构化输出必须携带 evidence（引用的 API 条目），
   验证器回查 API 交叉校验后才可采信。禁止绕过验证器直接落库。
7. **搜索只产候选**：任何网络搜索/联网工具的结果只能用于生成候选，最终匹配必须经
   元数据 API 直连验证。
8. **文件名与 NFO 永远一致**：二者由同一个 placement 决策生成，禁止出现只改一边的
   代码路径。

## 工作流约定

- 多步变更走 feature 分支（全局规则）。
- 提交信息用 Conventional Commits：`type(scope): subject`，默认分支 `main`。
  常用 type：`docs` / `feat` / `fix` / `chore` / `ci` / `test` / `refactor`；
  常用 scope：`parser` / `matcher` / `organizer` / `ledger` / `review` / `llm` /
  `metadata` / `scanner` / `api` / `web` / `db` / `nix` / `docs` / `domain` /
  `task` / `config` / `auth` / `deployments`。
- **文档先行**：任何改变设计决策的变更，先在 `docs/DECISIONS.md` 追加/修订 ADR，
  再改代码。代码与文档冲突时，以"先修文档再改代码"的顺序解决。
- **schema 唯一事实源是迁移文件**：`docs/ARCHITECTURE.md` §12 是设计基准，
  M0 起若与迁移文件漂移，以迁移为准并必须立即同步修订文档。
- 里程碑完成时必须更新 `docs/ROADMAP.md` 的"当前进度"节与相关文档。
- 新增外部依赖前确认必要性（Go 标准库优先）；新增外部 API 前先在
  `docs/RESEARCH.md` 登记调研结论（认证方式、限流、稳定性）。
- 数据库 schema 变更必须通过迁移（migration）实现，禁止手改 SQLite 文件；
  迁移文件一旦提交不可修改，只能追加新迁移。

## 测试分工（重要）

- **开发容器内**（本环境）：单元测试、golden 测试（解析器 fixture、NFO 黄金样本、
  软链接布局模拟）、API 测试。容器内**无法**运行 podman/docker/Jellyfin。
- **宿主机**（由用户执行）：真实媒体服务器集成测试。每个里程碑的宿主机测试计划与
  检查单见 `docs/ROADMAP.md` 与 `docs/testing/`。交付物为"二进制直跑（迭代）+
  OCI 镜像（集成测试）"双轨。
- 因此：涉及真实服务器行为（Jellyfin/Emby 解析、转码、字幕拾取）的假设，必须标注为
  "待宿主机实测（注明对应里程碑）"，不得当成已验证事实写进代码断言。
  已验证的结论见 `docs/RESEARCH.md` §7。

## 目录结构（现状；llm/organizer/ledger 暂为 doc.go 占位，按 ROADMAP 逐步实现）

```
cmd/roxy/          入口（serve/version；deps.go 装配流水线与调度）
cmd/fixturegen/    测试假库生成器（M1 夹具；-probe 探针模式）
internal/
  config/          YAML 配置 + ROXY_* 环境变量覆盖 + 校验
  auth/            会话认证（bcrypt、会话存储、初始引导）
  domain/          实体与状态机 + 文本归一化（半角/比对键）
  db/              SQLite + 迁移（schema 唯一事实源）
  scanner/         源发现（dirscan v1；qbit/transmission v2）
  parser/          文件名解析规则引擎（LLM 兜底 M3）
  metadata/        bangumi 客户端 + Archive dump 导入 + FTS 检索（anilist/tmdb M3）
  matcher/         系列解析、集映射、置信度（验证器 M3）
  task/            异步任务运行器（单 worker，kind→handler 注册表）
  llm/             Chat Completions 抽象、schema 校验、重试
  organizer/       软链接 / NFO / 图片 / 字幕配对
  ledger/          台账、返工、漂移对账
  review/          审核队列（M2 骨架）、反馈笔记（M5）
  api/             REST（M2 已实现：认证/health/sources/files/review/tasks/index）；SSE 排 M3
web/               React + TS + antd 前端（go:embed 经 -tags=web 内嵌，D-040/D-041；
                   默认构建为占位页，生产一律 nix build）
deployments/       podman/（test-up/test-down 已交付）→ quadlet/ + compose/（M6）
testdata/          解析器 golden（parser/cases.jsonl）与索引夹具（archive/*.jsonlines）
docs/              架构、决策、路线图、调研、测试文档
```

## 环境备忘

- 本仓库位于 `/data/projects/hinnyuu/roxy`；宿主机测试目录为仓库内 `test_share/`
  （用户管理，已 gitignore）。
- 工作区与宿主机**同路径共享**（宿主机 VSCode 已连接 GitHub，对 hinnyuu/roxy
  有全部权限）；**容器内无 GitHub 凭据**。
- **git 推送流程**：agent 完成本地提交 → 通知用户 → 用户在宿主机执行
  `cd /data/projects/hinnyuu/roxy && git log --oneline -3 && git push -u origin main`。
  agent 不得自行配置凭据或尝试推送。
- 上游仓库：`https://github.com/hinnyuu/roxy`（组织 hinnyuu，AGPL-3.0）。
- 镜像仓库：`ghcr.io/hinnyuu/roxy`（视同 git 远程，推送需用户确认）。
