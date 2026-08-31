# roxy 路线图（M0–M6）

> 每个里程碑含：交付物、容器内验收（agent 可自证）、宿主机测试计划（用户执行）。
> 测试分工见 `docs/testing/README.md`。宿主机检查单在各里程碑启动时于
> `docs/testing/` 落地（如 `m1.md`）。

---

## 当前进度（项目状态单一事实源，随里程碑实时更新）

- **M0：验收通过**（2026-08-29）。
- **M1：完成**（2026-08-30）——双服务器实测回填，命名规范主体冻结（D-036），
  Emby `-trailer` 红线（D-037）；发现 Jellyfin 多版本不合并（D-038 立项）。
- **M1.5：完成**（2026-08-30）——Jellyfin 三变体探针全部不合并，
  D-038 定稿 + D-039 版本仓库机制确立（迁移 0002 落地）。
- **M2：完成**（2026-08-30）——容器内验收 + 宿主机测试 10/10 通过（回填见
  docs/testing/m2.md §9）；过程中修复：401 探测重定向风暴、深色主题白底、
  标题候选半角化（均见对应 fix 提交）。
- **M2.5：完成**（2026-08-30）——高置信错判加固：D-042 同名多候选封顶、
  D-043 系列首确认（默认开启）；真实 dump 加固测试全绿。
- **M2.5 宿主机确认通过**（2026-08-30）：已推送，宿主机确认"全新系列的第一个
  文件进入待审核队列"（D-043 首确认）按预期生效。
- **当前阶段：可以开始 M3**。前置检查：确认最近一次推送的 GitHub Actions CI
  为绿、`ghcr.io/hinnyuu/roxy:dev` 已含 M2.5（以上述首确认行为验证）。
- 下一步动作：M3（LLM 层 + 多元数据源 + 审核队列交互化）。
- README.md 的"项目状态"节指向本节；禁止在其他位置重复维护进度。

---

## 测试分工速览

| 环境 | 能力 | 内容 |
|---|---|---|
| 开发容器（agent） | Go 工具链、Nix、无嵌套容器 | 单元测试、golden 测试、API 测试、`nix build` 冒烟 |
| 宿主机（用户） | podman、Jellyfin 10.11.11、Emby 4.9.5、真实媒体 | 集成测试、兼容性实测、端到端演练 |

交付双轨：**二进制直跑**（宿主机快速迭代）+ **OCI 镜像**（`ghcr.io/hinnyuu/roxy`，
集成测试与最终形态）。

---

## M0 — 仓库骨架与交付管道

**交付物**
- `flake.nix` 三输出（devShell / packages.default / OCI 镜像）+ `flake.lock`
- 目录骨架（见 AGENTS.md）；`cmd/roxy` 最小入口（--help/--version）
- SQLite 迁移框架 + ARCHITECTURE.md §12 全量 schema 落地
- 配置加载（YAML + `ROXY_*` 环境变量）；认证模块（admin/admin + 警告条）；空 REST 骨架
- `.github/workflows/`：PR 测试；main 合并构建并推送 `ghcr.io/hinnyuu/roxy:dev`

**容器内验收**
- `go build ./...`、`go test ./...`、`go vet`、`gofmt -l .` 全绿
- `nix build` 后 `./result/bin/roxy --help` 正常
- 迁移可正向执行并幂等重放

**宿主机测试**
- 无功能可测；仅验证 `podman pull ghcr.io/hinnyuu/roxy:dev` 成功、
  `podman run --rm … --help` 正常退出。

---

## M1 — 兼容性测试桩（命名规范冻结实验）

**交付物**
- 假库生成脚本：生成 KB 级哑媒体文件 + 手写 NFO，覆盖边界用例：
  `Season 00` 特别篇、多版本同集、`S01E01E02` 多集合一、`Extras/` 目录、
  剧场版电影条目、字幕 sidecar（含多语言后缀）
- `docs/testing/m1.md` 检查单（预期结果表 + 日志收集命令）

**容器内验收**
- 生成脚本单测：目录树与文件命名符合模板

**宿主机测试（关键里程碑）**
1. 建 `test_share/`，运行生成脚本产出 `test_share/library` 假库
2. `podman run jellyfin -v test_share:/media:ro` → 建库（指向 `/media/library/tv`
   与 `/media/library/movies`，禁用刮削器）→ 扫描
3. Emby 4.9.5 重复同样步骤
4. 回填检查单：季/集识别、S00 归属、**多版本是否合并为单条目**、
   字幕拾取与语言识别、extras 识别、服务器是否向库目录写入
5. 结论回传 → **冻结命名规范**（更新 ARCHITECTURE.md §4 状态）

---

## M2 — 扫描 + 规则解析 + 本地索引（无 LLM 半自动流程）

**交付物**
- Scanner：DirScanProvider（WebUI 手动触发 + 可选定时）
- Parser 规则引擎：发布组命名模板、中括号提取、集数模式（`[01]`/`- 01`/
  `第01话`/`EP01`/`01v2`/`12.5`）、version_key 归一化、字幕语言标签映射
- Bangumi 客户端（匿名 + 规范 UA）+ **Archive dump 导入（本地 FTS 索引）**
- 规则匹配 + 审核队列骨架；WebUI 骨架（登录/源管理/队列只读）
- `deployments/podman/test-up.sh` / `test-down.sh`（宿主机测试拓扑一键起/拆）

**容器内验收**
- 解析器 golden 测试（testdata/ 命名样本，含 VCB 风格复合括号用例）
- dump 导入测试（用裁剪的小型样本）；索引检索测试

**宿主机测试**
1. `podman pull ghcr.io/hinnyuu/roxy:dev`（或二进制直跑）
2. `deployments/podman/test-up.sh` 起拓扑 → 打开 `127.0.0.1:8080`
3. 添加 `test_share/downloads` 为源 → 手动扫描
4. 验收：文件入库、解析结果展示、规则命中的候选进入队列
5. 顺带验证 Archive dump 实际体积与导入耗时

---

## M3 — LLM 层 + 多元数据源

**交付物**
- LLM Provider 抽象（Chat Completions、三级降级、多 provider 降级、日志全留）
- 三个决策点 schema + 验证器 + 置信度分级 + 自动放行阈值
- AniList 适配器（批量 + 缓存）、TMDB 适配器（图片 + ID 桥接）
- 升级搜索（原生搜索能力位 + roxy 自搜索注入）；新番预热缓存
- WebUI：审核队列可交互（批准/驳回/附提示返工/改派）

**容器内验收**
- schema 校验与验证器单测（mock LLM 响应）
- 置信度计算单测；provider 降级逻辑测试

**宿主机测试**
1. 配置 LLM key（环境变量）与 TMDB key
2. 运行"刁钻命名 fixture 清单"（含 `[Nekomoe kissaten&VCB-Studio] …` 等真实样本）
3. 验收：自动放行/人工队列分界合理；LLM 日志页可见证据链；
   纠正后二次扫描行为符合预期

---

## M4 — Organizer 端到端（链接/NFO/图片/字幕配对）

**交付物**
- Organizer：相对软链接、NFO 生成（tvshow/season/episode/movie）、
  图片下载、字幕版本配对；全部幂等、记台账
- 漂移巡检（reconcile）；悬空链接检测
- 系列页基础视图（季/集/版本树）

**容器内验收**
- NFO golden 测试（黄金样本 XML）
- 软链接布局模拟测试（临时目录树：相对链接可达性、重放幂等、回滚精确性）

**宿主机测试（端到端）**
1. 真实 BT 资源放入 `test_share/downloads`（含多版本 + 分离字幕目录场景）
2. 扫描 → 审核/自动放行 → 验证 `library/` 产出（链接可达、NFO 内容、图片）
3. Jellyfin/Emby 扫描 roxy 生成的库 → 条目、季、集、版本、字幕全部正确
4. 漂移演练：移走一个源文件 → 巡检告警 → 清理；恢复后重建

---

## M5 — WebUI 完整版（人工闭环）

**交付物**
- 返工流程全量（标记/回滚/重提案）；三级策略覆盖 UI + diff 预览
- NFO 编辑器（表单 + 原始 XML 双模式，manual_lock 可见）
- 反馈笔记管理；仪表盘；台账浏览器；任务中心
- （可选）Bangumi OAuth2 流程（收藏同步预热）

**容器内验收**
- API 层测试覆盖全部端点；状态机转换测试

**宿主机测试**
- 按"返工演练脚本"执行故意出错场景：认错系列、认错集、策略覆盖
  （"此番 OVA 进 S00"）、NFO 手编后重扫不覆盖、反馈笔记生效验证

---

## M6 — 下载客户端集成 + 长期部署形态

**交付物**
- QBittorrentProvider（WebAPI 轮询/完成事件）+ TransmissionProvider（RPC）
- `deployments/quadlet/`（podman 常驻）与 `deployments/compose/`（docker）
- release 工作流（tag → 版本镜像 + GitHub Release）；用户文档补全

**容器内验收**
- provider 契约测试（录制回放 qbit/tr API 响应）

**宿主机测试**
- 真实下载 → 完成事件 → 自动整理全链路；quadlet 常驻重启稳定性

---

## 里程碑间通用规则

- 每个里程碑结束：更新本文件的实际进度；新发现的服务器行为写入
  `docs/RESEARCH.md` 的"待验证清单"并标注结论。
- 宿主机测试失败时：收集检查单指定的日志 → 反馈 → 容器内复现（若可）→
  修复 → 重新走该里程碑验收。
- 任何设计变更先改 `docs/DECISIONS.md`（见 AGENTS.md）。
