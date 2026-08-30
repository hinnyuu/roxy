# roxy 架构文档

> 状态：**M1/M1.5 实测冻结版**。命名规范主体已冻结（D-036），版本仓库机制定稿
> （D-038/D-039）；唯一剩余的未实测例外是**字幕拾取行为**（待 M4 真实媒体实测）。
> 变更本文档须同步更新 `docs/DECISIONS.md`。

---

## 目录

1. [目标与非目标](#1-目标与非目标)
2. [部署形态](#2-部署形态)
3. [总体架构](#3-总体架构)
4. [目录结构与命名规范](#4-目录结构与命名规范)
5. [核心实体模型](#5-核心实体模型)
6. [匹配流水线](#6-匹配流水线)
7. [LLM 设计](#7-llm-设计)
8. [元数据源](#8-元数据源)
9. [版本模型与字幕配对](#9-版本模型与字幕配对)
10. [策略覆盖与返工流程](#10-策略覆盖与返工流程)
11. [认证与安全](#11-认证与安全)
12. [SQLite Schema（全量）](#12-sqlite-schema全量)
13. [配置结构](#13-配置结构)
14. [REST API 端点清单](#14-rest-api-端点清单)
15. [仓库结构](#15-仓库结构)

---

## 1. 目标与非目标

### 目标

- 为 Jellyfin（主力）/ Emby / Kodi（次选）提供**正确**的动漫媒体库：
  TV 版、剧场版、OVA、ONA、特别篇、NCOP/NCED、PV/CM 各得其所；
  第 0 集、12.5 集、FINAL 集等不规则编号正确归位。
- **零破坏**：BT 下载源文件永不被移动、改名、删除；做种永不中断；空间零膨胀。
- **人机协作**：LLM 处理规模化识别，人工保留最终决定权与纠错通道，纠错可沉淀。
- 单用户自托管，podman/docker 友好，单二进制部署。

### 非目标

- 不做媒体播放、转码、串流（那是服务器的事）。
- 不下载任何媒体内容（不做字幕下载器、不做 BT 客户端）。
- 不处理音乐、图书、真人影视（v1 仅限动漫；架构不排斥未来扩展）。
- 不支持 SMB/NFS 网络共享给 Kodi 的场景（软链接跨机器解析不可行，明确排除）。
- 不做多用户/权限体系（单用户自用）。

## 2. 部署形态

### 单一公共父目录（官方唯一形态）

所有容器以**相同结构**挂载同一个公共父目录（示例中为 `/media`）：

```
/media/                          ← 公共父目录（宿主机如 ~/test_share 挂载为 /media）
├── downloads/                   ← BT 下载目录：qBittorrent/Transmission rw，其余容器 ro
└── library/                     ← roxy 输出：roxy rw，Jellyfin/Emby/Kodi ro
```

podman 测试拓扑（M1–M5 阶段，CLI 直管）：

```
podman run roxy:      -v test_share:/media:rw  + 配置卷 + DATA_DIR 卷 + 端口 127.0.0.1:8080
podman run jellyfin:  -v test_share:/media:ro
podman run emby:      -v test_share:/media:ro
(未来) podman run qbittorrent: -v test_share:/media:rw
```

注意：SELinux 环境使用 `:z`/`:Z` 挂载选项；测试期端口仅绑定 `127.0.0.1`。

### 相对软链接为何成立

相对符号链接按"链接所在目录"解析，与挂载点绝对路径无关。只要各容器内
`downloads/` 与 `library/` 的兄弟关系一致，链接在所有容器内均可解析——
**路径映射配置因此不再必需**。roxy 用 `filepath.Rel` 自动计算相对目标。

兜底代码路径：`media.link_mode: absolute` + `path_mappings` 映射表
（下载与库无法保持兄弟关系的罕见布局），默认关闭。

### 媒体服务器侧配置要求

- 禁用全部在线刮削器/元数据下载器，仅启用 NFO 读取（Jellyfin 内置 Kodi NFO 插件）。
- 关闭"将元数据/图片保存到媒体目录"（roxy 已提供全部 sidecar 文件）。
- `library/tv` 与 `library/movies` 分别建为剧集库与电影库。

## 3. 总体架构

六层流水线 + 横切设施：

```
┌──────────────────────────────────────────────────────────────┐
│  WebUI（React+TS，go:embed 内嵌）                              │
│  仪表盘 / 审核队列 / 系列详情 / 源管理 / NFO 编辑器 /            │
│  台账浏览器 / 任务与 LLM 日志 / 设置                             │
├──────────────────────────────────────────────────────────────┤
│  API 层：REST + SSE（/api/*，会话认证）                          │
├──────────────────────────────────────────────────────────────┤
│  Review 层：置信度分级 → 高置信自动放行 / 低置信入人工队列         │
│            审核、驳回、改派、附提示返工、反馈笔记回流              │
├──────────────────────────────────────────────────────────────┤
│  Matcher 层：系列解析（本地索引 + 在线检索）→ 集映射              │
│             → LLM 带证据决策 → 验证器交叉校验 → 置信度计算        │
├──────────────────────────────────────────────────────────────┤
│  Parser 层：规则引擎（发布组命名模板、中括号、集数模式）           │
│             解析失败/低置信 → LLM parse schema 兜底              │
├──────────────────────────────────────────────────────────────┤
│  Scanner 层：SourceProvider 接口                               │
│             v1: DirScanProvider（WebUI 手动触发 + 定时扫描）     │
│             v2: QBittorrentProvider / TransmissionProvider     │
├──────────────────────────────────────────────────────────────┤
│  Organizer 层：创建相对软链接 / 生成 NFO / 下载封面 /             │
│               字幕版本配对 —— 全部幂等、全部记入台账               │
└──────────────────────────────────────────────────────────────┘
横切：SQLite（实体+台账+索引）｜ LLM Provider 抽象 ｜ 元数据适配器
     ｜ 任务队列（tasks 表）｜ 漂移巡检（定期对账）
```

关键设计原则：

1. **触发归一化**：无论手动扫描、定时扫描还是未来的下载完成事件，都归一为
   `SourceEvent` 进入同一流水线。
2. **幂等与可回滚**：任何整理操作可重放；产出物全部登记台账，回滚=按台账删除。
3. **LLM 决策点最小化**：规则能解决的不调 LLM；LLM 输出必须过 schema 校验与
   证据验证；验证失败降级人工。
4. **收敛点是实体不是目录**：文件何时到达、来自哪个源目录不重要，匹配到同一
   Series 实体即归入同一媒体库目录。

## 4. 目录结构与命名规范

> 状态：**实测冻结**（M1 2026-08-30，Jellyfin 10.11.11 + Emby 4.9.5，
> 记录见 `docs/testing/m1.md`，决策 D-036；版本后缀形态经 M1.5 探针定稿，
> 见 D-038/D-039）。
> 唯一未决例外：**字幕拾取行为**待 M4 真实媒体实测。
> 任何命名变更必须先修 `docs/DECISIONS.md` 并重列实测清单。

### 媒体库布局

```
library/
├── tv/
│   └── 某番剧 (2024)/                 # 目录名 = 中文标题 + 年份
│       ├── tvshow.nfo                 # 实体文件
│       ├── poster.jpg  fanart.jpg  banner.jpg  logo.png   # 实体文件（下载）
│       ├── Season 00/                 # 特别篇（第0集/总集篇/FINAL/NCOP/NCED 按策略）
│       ├── Season 01/
│       │   ├── S01E01 - 第01话 标题.mkv          → 相对软链接
│       │   ├── S01E01 - 第01话 标题.nfo          # 实体文件
│       │   ├── S01E01 - 第01话 标题.zh-CN.srt    → 相对软链接
│       │   └── （库内只有主版本、无后缀；非主版本见下方 vault/ 示例，D-039）
│       └── Extras/                    # PV/CM（Jellyfin 识别为花絮；Emby 并入特别篇，D-036/037）
└── movies/
    └── 某番剧 剧场版 某某 (2025)/
        ├── 某番剧 剧场版 某某 (2025).mkv → 相对软链接
        ├── 某番剧 剧场版 某某 (2025).nfo
        └── poster.jpg  fanart.jpg

vault/                              # 版本仓库（D-039）：library 的兄弟目录，
└── tv/                             # 不在任何媒体库扫描路径内
    └── 某番剧 (2024)/Season 01/
        └── S01E03 - 第03话 [BetaSub].mkv → 相对软链接（非主版本）
```

### 命名规则（机器惯例优先，自由度全部放 NFO）

| 层 | 规则 | 理由 |
|---|---|---|
| 剧集目录 | `Season 00`、`Season 01`（两位数字） | 三家服务器解析器硬编码识别的模式 |
| 剧集链接名 | `S{s:02}E{e:02} - {episode_title}` | resolver 先于 NFO 运行，文件名必须自解释；**文件名与 NFO 由同一决策生成，永远一致** |
| 多版本后缀 | **vault 模式（默认）**：主版本无后缀，vault 内版本带 ` [{version}]`；tolerate 模式：全体版本带后缀 | Jellyfin 对剧集多版本无合并路径（M1/M1.5 实测），默认版本仓库策略（D-038/D-039） |
| 多集合一文件 | `S01E01E02 - …`（M1 实测两家均识别为单一条目） | — |
| 电影目录/文件 | `{标题} ({年份})` | Jellyfin 电影库惯例（M1 实测通过） |
| 番外类文件（PV/CM/预告） | **只进 `Extras/` 子目录，禁止放剧集根目录** | Emby 会把根目录 `-trailer` 文件收为剧集（D-037 红线） |
| NFO 内容 | 完全自由：中文集标题、放送日期、绝对序号、Bangumi 评分、制作组等 | 人类可读层 |

不规则编号的归位策略：第 0 集 / 12.5 总集篇 / FINAL 集 → 映射为 `Season 00`
特别篇（Bangumi ep_type=1 / TVDB S00 体系），NFO 中写明原始语义。

### M1 实测清单（2026-08-30 完成，结果见 docs/testing/m1.md §6）

- [x] `Season 00` 与 `Specials` 目录名：两家均识别为特别篇（Season 0）
- [x] `S01E01E02` 多集合一：两家均识别为单一条目
- [x] extras：Jellyfin 识别 `Extras/` 为花絮、忽略根目录 `-trailer`；
      **Emby 把根目录 `-trailer` 收为剧集（红线，D-037）**，`Extras/` 并入特别篇
- [x] 零写入：只读挂载下两家均无写入
- [x] 中文与特殊字符（Ω＆Δ「」~）：两家均正常显示
- [x] **同集多版本合并：Jellyfin 无任何合并路径（M1.5 三变体全部失败）——
      版本仓库策略定稿（D-038/D-039）**
- [ ] 字幕拾取与版本配套字幕——假文件无法触发，延至 M4
- [ ] NFO uniqueid 于"识别"页不可见（非阻塞，见 D-036）

## 5. 核心实体模型

```
Source          监控根（目录 + 类型提示 mixed/video/subtitle；provider 配置）
SourceFile      扫描到的文件（路径、大小、mtime、kind、解析结果、状态）
Series          系列 = Bangumi subject；持外部 ID（anilist/tmdb/imdb）、别名表、
                输出目录、封面、策略覆盖；franchise 经 parent_series_id 关联
Placement       决策：某 SourceFile → 某 Series 的槽位
                （season/episode/slot_type/version_key），含证据与置信度
Ledger          物料台账：每个产出物路径 + 类型 + 来源决策 + 状态
ReviewCase      审核工单（含 LLM 日志引用、用户批注）
FeedbackNote    用户纠正/建议（global/series/pattern 三种作用域，注入 prompt 或规则）
Task            异步任务（scan/match/materialize/rework/reconcile/index_refresh）
LLMLog          每次 LLM 调用的完整请求/响应（WebUI 可查，调试与反馈落点）
```

### Placement 状态机

```
new → parsed → proposed(匹配+置信度分解)
    ├─ 置信度 ≥ 阈值 ──► auto_approved ─┐
    └─ 置信度 < 阈值 ──► pending_review → 人工批准 / 驳回 / 附提示重跑 / 改派
                                          │
                              approved ◄──┘
                                  │
                          materialized（建链接/写NFO/下图，记台账）
                                  │
                    [事后发现问题：人工标记 / 漂移巡检 / 策略变更]
                                  ▼
                              flagged → reworking（按台账回滚）→ 回到 proposed
```

`manual_lock`：人工编辑过的 NFO/槽位打锁，自动流程永不覆盖，解锁须显式确认。

## 6. 匹配流水线

```
1. 系列解析（这是谁？）
   a. 本地 Series 别名匹配（归一化标题：去标点/大小写/全半角）→ 命中即收敛
   b. 本地 Bangumi 索引 FTS 检索（Archive dump）→ 候选
   c. 在线检索（Bangumi API / AniList GraphQL / TMDB）→ 候选
   d. 候选 ≥2 或全空 → LLM match schema 决策（含 evidence）；
      仍不确定 → 升级搜索（见 §7.4）→ 仍不确定 → 人工队列
   e. 验证器：按 LLM 给出的 ID 直连 API 核对标题/年份/类型 → 通过才采信

2. 集映射（这是第几集/哪类内容？）
   a. Parser 已给出 ep_number_raw 与 ep_type_hint
   b. 对照该 Series 的集列表（Bangumi episodes：sort + ep_type
      0正篇/1特别篇/2OP/3ED/4Trailer/5MAD/6其他）
   c. 歧义（绝对序号 vs 季度序号、总集篇、分割放送）→ LLM mapping schema
   d. 验证：集号必须存在于 API 集列表或落入 S00 特别篇范围

3. 置信度分解（可解释）
   标题匹配分 + API 证据一致分 + 规则命中强度 + LLM 自报置信度
   → 加权得总分；≥ auto_approve_threshold(0.90) 自动放行，否则人工

4. 落盘（Organizer）
   按生效策略（手动指定 > 系列覆盖 > 全局策略）生成路径 → 原子创建 → 记台账
```

## 7. LLM 设计

### 7.1 Provider 抽象

- 统一走 **OpenAI 兼容 Chat Completions**（`/v1/chat/completions`）——
  Qwen（Alibaba Token Plan）、DeepSeek 及其他兼容端点的最大公约数。
- **不使用 Responses API**（供应商私有，收益不抵锁定）。
- **不内嵌任何外部 agent CLI**：roxy 的"agent"是确定性流水线 + 少数 LLM 决策点；
  远期可将 roxy 自身暴露为 MCP server 供外部 agent 驾驶（非目标，不设时间表）。
- 多 provider 按 priority 降级；每个 provider 声明能力位
  （`native_search`、`function_calling`）。
- function calling 作为 v1.x 升级路径保留（疑难样本多步推理）。

### 7.2 三个决策点 schema（均强制 evidence 字段）

```
parse:   {title_candidates[], ep_number_raw, ep_type_hint, release_group,
          version_info, confidence}
match:   {series_id, evidence: {api, id, matched_fields[]}, confidence, reasoning}
mapping: [{file, season, episode, slot_type(tv|special|movie|op|ed|pv|cm|extra),
           evidence}]
```

### 7.3 结构化输出三级降级

`response_format: json_schema`（支持的供应商）→ `json_object` →
纯 prompt 约束 + 解析失败自动重试（上限可配）。所有输出先过 schema 校验，
再过验证器，两关都过才有效。

### 7.4 搜索三层设计（搜索是 roxy 的能力，不是模型的能力）

1. **原生搜索（可选加速器）**：provider 支持则启用（如 Qwen/DashScope 的
   `enable_search`，实现时实测确认）。
2. **roxy 自搜索 + 注入（通用兜底）**：roxy 自行检索并把结果注入 prompt，
   模型只推理。优先用结构化"准搜索"：Bangumi 检索、AniList 季度查询、
   本地新番预热缓存（每季自动缓存当季/下季番列表），通用搜索引擎 API 为
   可选扩展（默认关闭）。
3. **铁律**：搜索只产候选；最终匹配必须直连元数据 API 验证。搜索结果缓存入库
   （search_cache 表），同一问题不重复付费。

### 7.5 成本控制

规则引擎先行（发布组命名高度结构化，80% 零成本覆盖）；匹配结果按
（归一化标题 + 版本签名）缓存；LLM 日志全量留存供复盘。

## 8. 元数据源

### 8.1 职责分工

| 源 | 角色 | 认证 | 备注 |
|---|---|---|---|
| **Bangumi** | 主力：中文标题/简介、集列表与集分类、评分、标签、关联关系 | 匿名读接口可用（限流较严）；必须带规范 User-Agent | 无个人令牌页；OAuth2 流程排 M5/M6（收藏同步才需要） |
| **bangumi/Archive** | Bangumi 全量周更 dump → **本地 SQLite FTS 索引** | 无需认证（GitHub Releases） | 候选检索本地化，零限流；在线 API 只做增量/图片/核验 |
| **AniList** | 新番鉴别、别名（synonyms）、交叉验证、关系图遍历 | 读接口无需 token | GraphQL 单端点；限流 90/min（当前降级 30/min），必须批量+缓存 |
| **TMDB** | 图片（海报/背景图 CDN）、tmdb/imdb ID 桥接、电影条目 | 免费 API key | 图片**下载到本地**存放，播放零外链依赖 |
| TVDB | 不接入（API 订阅制） | — | 架构留适配器槽位 |
| IMDB | 不直连（无官方 API） | — | imdb_id 仅经 TMDB 外部 ID 顺带获取 |

### 8.2 NFO 字段映射

| NFO 字段 | 首选来源 | 备选 |
|---|---|---|
| title / originaltitle / plot | Bangumi | AniList / TMDB |
| uniqueid（default=bangumi，附 tmdb/imdb） | 各 API | — |
| season/episode（= placement 决策） | 匹配流水线 | — |
| aired / premiered | Bangumi（airdate） | AniList |
| rating | Bangumi score | — |
| genre / tag | Bangumi tags + meta_tags | AniList genres/tags |
| studio | Bangumi（infobox 解析，可选，wiki-parser-go） | AniList studios |
| 海报/背景图 | TMDB 图床 | Bangumi 图片接口（302） |

### 8.3 本地 Bangumi 索引（Archive dump）

- 来源：`github.com/bangumi/Archive` releases（tag `archive`），
  最新地址经 `aux/latest.json` 解析（含 sha256 digest 可校验）；
  每周三 05:00 (GMT+8) 更新。
- 载体格式（2026-08-30 核验，详见 RESEARCH.md §2）：单一 zip（约 435MB）内含
  9 个 JSONL；导入为流式两趟：先 `subject.jsonlines` 过滤 type=2 建动画 ID 集，
  再按集过滤 `episode` / `subject-relations`。
- 导入范围：动画条目（type=2）+ 章节 + 条目关联；进 `bgm_subjects /
  bgm_episodes / bgm_relations` 表 + FTS5（导入事务内摘除/重建 FTS 触发器，
  批量插入后 `rebuild`，避免百万行逐行触发）。
- 刷新：`index_refresh` 任务，默认每周自动（可关）；检索为 unicode61 分词，
  中文走前缀查询 + LIKE 兜底两级（trigram 为后备升级路径）；SQLite 索引实际
  体积与导入耗时 M2 实测回填（磁盘无约束，宿主机 3TB SSD）。
- infobox 原始 wiki 字符串暂不解析（需要时用官方 `bangumi/wiki-parser-go`）。

## 9. 版本模型与字幕配对

### 9.1 版本（同集多压制版并存）

实测事实：**Jellyfin 对剧集多版本无任何合并路径**（方括号与 " - " 分隔后缀
均不合并，M1/M1.5 实测）；Emby 可合并。因此默认策略为**版本仓库**
（D-038/D-039）：

**vault 模式（默认，`policy.multi_version: vault`）**

- 主版本 = 先到者，链接进 `library/`，**无后缀**（库内单一版本）；
- 其余版本的软链接进 `vault/tv/{番剧} ({年份})/Season XX/`，带
  ` [{version}]` 后缀；vault 是 library 的兄弟目录，天然在服务器扫描路径外；
- 系列页"设为主版本" = 主版本与目标 vault 版本的纯链接互换（台账驱动）；
- 字幕链接跟随其视频链接：主版本字幕在 library，vault 版本的配套字幕
  跟随进 vault。

**tolerate 模式（可配置）**：全体版本在库内并存、全部带 ` [{version}]`
后缀（Jellyfin 呈重复条目、各自按 basename 配字幕；Emby 合并为多版本）。

**版本识别与配对（两种模式通用）**

- 解析阶段提取 `release_group`（首个中括号的原始字符串，归一化后作
  **version_key**）与文件名中的 CRC 哈希（动漫压制惯例）。
- **配对不依赖语义分类**：视频与配套字幕共享相同文件名前缀/中括号，
  原始字符串相等即可配对——无论括号内是"字幕组&压制组"还是多组合作。
- LLM 语义标注（谁是字幕组/谁是压制组）只服务显示层，结果缓存入库，
  错了不影响配对，可纠正。

### 9.2 字幕配对降级链

```
0. 字幕 basename == 视频 basename + 语言标签        → 精确配对（最强信号）
1. 同 version_key + 同集号                          → 挂到该版本
2. 只定得了集、定不了版本                            → 挂到该集所有版本 + 审核队列提示
3. 无法确定                                          → 人工队列
```

### 9.3 语言标签映射（可编辑）

VCB 系等惯例标签 → 标准后缀：`JPSC→zh-CN`、`JPTC→zh-TW`、`SC→zh-CN`、
`TC→zh-TW`、`JP→ja`、`CHS→zh-CN`、`CHT→zh-TW`。映射表存 settings，WebUI 可改。

## 10. 策略覆盖与返工流程

### 10.1 三级策略覆盖

```
决策 = 单文件手动指定（manual） > 系列级覆盖（series.policy_overrides）
     > 全局默认（config.policy）
```

| 策略键 | 取值 | 默认 |
|---|---|---|
| `movie`（剧场版） | `separate`（movies/ 独立条目）/ `s00` | `separate` |
| `ova` | `separate`（独立番剧目录）/ `season` / `s00` | `separate` |
| `ncop_nced` | `s00` / `extras` / `skip` | `s00` |
| `pv_cm` | `extras` / `skip` | `extras` |
| `multi_version` | `vault`（版本仓库，D-039）/ `tolerate`（容忍重复条目） | `vault` |
| `auto_approve_threshold` | 0–1 | `0.90` |

### 10.2 策略变更与返工

- 系列页"重新应用策略"：按新策略重算全部 placement → **生成 diff 预览**
  （将移动/改名/删除的产出物清单）→ 用户确认 → 落盘。永不直接动文件。
- 返工 = 按台账精确回滚（删除登记的产出物）+ 重新进入流水线。
- `manual_lock` 项在重算中保持不动，除非显式解锁。
- **漂移巡检**（reconcile 任务）：定期对账台账与文件系统——链接目标消失
  （删种）、链接被手动删除、库内出现不明文件 → 生成待处理工单。

### 10.3 反馈回流

用户每次纠正落为 FeedbackNote：
- `series` 作用域："这个系列的 '08' 是绝对序号" → 注入该系列后续 prompt；
- `pattern` 作用域："该字幕组命名规则是…" → 沉淀为规则引擎条目；
- `global` 作用域：通用偏好。
WebUI 的 LLM 日志页是"给 LLM 提建议"的落点（从某次失败调用直接发起纠正）。

## 11. 认证与安全

- 会话认证：登录接口 → httpOnly cookie；密码 bcrypt 哈希入库。
- 初始凭据 `admin/admin`，支持 `ROXY_ADMIN_PASSWORD` 环境变量首启覆盖；
  **默认密码未修改时 WebUI 常驻警告条 + 启动日志警告**；设置页可改用户名/密码。
- 测试期 podman 端口仅绑定 `127.0.0.1`（比强密码更有效的隔离）。
- 预留扩展：API 自动化 token、只读角色（schema 已含 role 字段）。
- 机密管理：全部走 `ROXY_*` 环境变量；配置文件中只允许出现 `*_env: 变量名`
  的引用形式；日志脱敏。

## 12. SQLite Schema（全量）

> 以迁移文件形式落地（M0）。此处为设计基准。

```sql
-- 认证
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'admin',      -- admin | readonly(预留)
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

-- 源与文件
CREATE TABLE sources (
  id              INTEGER PRIMARY KEY,
  name            TEXT NOT NULL,
  path            TEXT NOT NULL UNIQUE,
  kind            TEXT NOT NULL DEFAULT 'mixed'
                  CHECK (kind IN ('mixed','video','subtitle')),
  provider_type   TEXT NOT NULL DEFAULT 'dirscan',  -- dirscan | qbittorrent | transmission
  provider_config TEXT,                             -- JSON，v2 客户端连接参数
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      TEXT NOT NULL
);

CREATE TABLE source_files (
  id           INTEGER PRIMARY KEY,
  source_id    INTEGER NOT NULL REFERENCES sources(id),
  abs_path     TEXT NOT NULL UNIQUE,
  size         INTEGER NOT NULL,
  mtime        TEXT NOT NULL,
  kind         TEXT NOT NULL DEFAULT 'unknown'
               CHECK (kind IN ('unknown','video','subtitle','nfo','image','other')),
  status       TEXT NOT NULL DEFAULT 'new'
               CHECK (status IN ('new','parsing','parsed','placed','ignored','error')),
  parse_result TEXT,                                -- JSON：parser 输出
  provenance   TEXT,                                -- JSON：未来 torrent 元数据（分类/tracker）
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);

-- 系列
CREATE TABLE series (
  id               INTEGER PRIMARY KEY,
  bgm_subject_id   INTEGER UNIQUE,
  anilist_id       INTEGER,
  tmdb_id          TEXT,
  imdb_id          TEXT,
  title            TEXT NOT NULL,
  title_original   TEXT,
  year             INTEGER,
  series_type      TEXT NOT NULL DEFAULT 'tv'
                   CHECK (series_type IN ('tv','ova','ona','movie','special','other')),
  parent_series_id INTEGER REFERENCES series(id),  -- franchise 关联
  library_kind     TEXT NOT NULL DEFAULT 'tv' CHECK (library_kind IN ('tv','movie')),
  library_path     TEXT,                           -- 输出目录（相对 library root）
  poster_path      TEXT,
  fanart_path      TEXT,
  policy_overrides TEXT,                           -- JSON：§10.1 策略键覆盖
  status           TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active','archived')),
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);

CREATE TABLE series_aliases (
  id        INTEGER PRIMARY KEY,
  series_id INTEGER NOT NULL REFERENCES series(id),
  alias     TEXT NOT NULL,
  source    TEXT NOT NULL CHECK (source IN ('api','user','learned')),
  UNIQUE (series_id, alias, source)
);

-- 决策
CREATE TABLE placements (
  id                       INTEGER PRIMARY KEY,
  source_file_id           INTEGER NOT NULL REFERENCES source_files(id),
  series_id                INTEGER NOT NULL REFERENCES series(id),
  slot_type                TEXT NOT NULL
                           CHECK (slot_type IN ('episode','special','movie','op','ed',
                                                'pv','cm','extra','subtitle','ignored')),
  season                   INTEGER,
  episode                  REAL,                   -- REAL 容纳 12.5
  episode_end              REAL,                   -- 多集合一文件的结束集（迁移 0003，D-036 S01E01E02）
  episode_title            TEXT,
  version_key              TEXT,                   -- 归一化原始中括号
  version_label            TEXT,                   -- LLM/人工标注的显示标签
  vault                    INTEGER NOT NULL DEFAULT 0,  -- 1=版本仓库内（D-039，迁移 0002 增补）
  subtitle_of_placement_id INTEGER REFERENCES placements(id),  -- 字幕→视频版本
  confidence               REAL,
  decision_source          TEXT NOT NULL CHECK (decision_source IN ('rule','llm','human')),
  evidence                 TEXT,                   -- JSON：引用的 API 条目
  review_state             TEXT NOT NULL DEFAULT 'proposed'
                           CHECK (review_state IN ('proposed','auto_approved',
                                                   'pending_review','approved',
                                                   'rejected','rework')),
  manual_lock              INTEGER NOT NULL DEFAULT 0,
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL
);

-- 物料台账
CREATE TABLE ledger (
  id            INTEGER PRIMARY KEY,
  placement_id  INTEGER REFERENCES placements(id),
  artifact_type TEXT NOT NULL CHECK (artifact_type IN ('symlink','nfo','image','dir')),
  path          TEXT NOT NULL UNIQUE,
  target        TEXT,                              -- symlink 的目标
  state         TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','removed')),
  created_at    TEXT NOT NULL
);

-- 审核与反馈
CREATE TABLE review_cases (
  id          INTEGER PRIMARY KEY,
  placement_id INTEGER NOT NULL REFERENCES placements(id),
  reason      TEXT,
  llm_log_id  INTEGER,
  user_note   TEXT,
  state       TEXT NOT NULL DEFAULT 'open'
              CHECK (state IN ('open','approved','rejected','reworked')),
  created_at  TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE feedback_notes (
  id          INTEGER PRIMARY KEY,
  scope       TEXT NOT NULL CHECK (scope IN ('global','series','pattern')),
  series_id   INTEGER REFERENCES series(id),
  pattern     TEXT,                                -- pattern 作用域的正则/模板
  note        TEXT NOT NULL,
  inject_into TEXT NOT NULL DEFAULT 'both' CHECK (inject_into IN ('prompt','rule','both')),
  created_at  TEXT NOT NULL
);

-- LLM 与搜索缓存
CREATE TABLE llm_logs (
  id          INTEGER PRIMARY KEY,
  task        TEXT NOT NULL,                       -- parse | match | mapping | label
  provider    TEXT NOT NULL,
  model       TEXT NOT NULL,
  request     TEXT NOT NULL,                       -- JSON（脱敏）
  response    TEXT NOT NULL,                       -- JSON
  tokens_in   INTEGER,
  tokens_out  INTEGER,
  duration_ms INTEGER,
  created_at  TEXT NOT NULL
);

CREATE TABLE search_cache (
  id         INTEGER PRIMARY KEY,
  query      TEXT NOT NULL,
  source     TEXT NOT NULL,                        -- bangumi | anilist | tmdb | web
  result     TEXT NOT NULL,                        -- JSON
  expires_at TEXT NOT NULL
);

-- 任务
CREATE TABLE tasks (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('scan','match','materialize','rework',
                                           'reconcile','index_refresh')),
  payload    TEXT,                                 -- JSON
  state      TEXT NOT NULL DEFAULT 'queued'
             CHECK (state IN ('queued','running','done','failed','cancelled')),
  progress   TEXT,                                 -- JSON
  error      TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- 本地 Bangumi 索引（Archive dump 导入）
CREATE TABLE bgm_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL                              -- dump 版本 / 导入时间 / 来源 URL
);

CREATE TABLE bgm_subjects (
  id      INTEGER PRIMARY KEY,                     -- Bangumi subject id
  type    INTEGER NOT NULL,                        -- 2=动画
  name    TEXT NOT NULL,
  name_cn TEXT,
  platform TEXT,                                   -- TV/剧场版/OVA/WEB…
  date    TEXT,
  score   REAL,
  rank    INTEGER,
  nsfw    INTEGER NOT NULL DEFAULT 0,
  summary TEXT
);

CREATE TABLE bgm_episodes (
  id         INTEGER PRIMARY KEY,                  -- Bangumi episode id
  subject_id INTEGER NOT NULL,
  name       TEXT,
  name_cn    TEXT,
  sort       REAL,                                 -- 集数（可小数）
  ep_type    INTEGER NOT NULL,                     -- 0正篇 1特别篇 2OP 3ED 4Trailer 5MAD 6其他
  airdate    TEXT
);
CREATE INDEX idx_bgm_episodes_subject ON bgm_episodes(subject_id, ep_type, sort);

CREATE TABLE bgm_relations (
  subject_id         INTEGER NOT NULL,
  related_subject_id INTEGER NOT NULL,
  relation_type      TEXT NOT NULL
);
CREATE INDEX idx_bgm_relations ON bgm_relations(subject_id);

-- FTS5 全文检索（external content 指向 bgm_subjects）
CREATE VIRTUAL TABLE bgm_subjects_fts USING fts5(
  name, name_cn, content='bgm_subjects', content_rowid='id'
);
```

## 13. 配置结构

YAML 主配置 + 环境变量覆盖；机密只允许 `*_env` 引用形式。

```yaml
server:
  host: 0.0.0.0            # 容器内监听；暴露范围由 podman -p 控制
  port: 8080
auth:
  mode: password           # 初始 admin/admin；ROXY_ADMIN_PASSWORD 可覆盖
data_dir: ./data           # 容器内建议 /data 卷

media:
  library_root: /media/library
  link_mode: relative      # relative（默认）| absolute
  path_mappings: []        # absolute 模式兜底：[{from: /media/downloads, to: /downloads}]

naming:
  show_folder: "{title} ({year})"
  episode: "S{s:02}E{e:02} - {episode_title}"
  version_suffix: " [{version}]"        # 仅多版本时附加
  movie: "{title} ({year})"

policy:                                 # 全局默认，可被系列级覆盖（§10.1）
  movie: separate
  ova: separate
  ncop_nced: s00
  pv_cm: extras
  multi_version: vault                  # vault | tolerate（D-039）
  auto_approve_threshold: 0.90

llm:
  providers:                            # 按 priority 降级
    - name: qwen
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      model: qwen-max
      api_key_env: ROXY_LLM_KEY_QWEN
      native_search: true               # 实现时实测确认
      priority: 1
    - name: deepseek
      base_url: https://api.deepseek.com/v1
      model: deepseek-chat
      api_key_env: ROXY_LLM_KEY_DEEPSEEK
      native_search: false
      priority: 2

metadata:
  bangumi:
    enabled: true
    user_agent: "RyougiShiki-214/roxy (https://github.com/hinnyuu/roxy)"
    token_env: ROXY_BGM_TOKEN           # 可选；无个人令牌，OAuth 排 M5/M6
    archive_index:
      enabled: true
      auto_refresh: weekly
  anilist:
    enabled: true                       # 无需 key；批量+缓存以耐受限流
  tmdb:
    enabled: true
    api_key_env: ROXY_TMDB_KEY

scanner:
  rescan_interval: 0                    # 0=仅手动触发（v1 默认）；秒数为定时
```

环境变量覆盖约定：`ROXY_` 前缀，层级以 `_` 连接（如
`ROXY_SERVER_PORT=9090`、`ROXY_MEDIA_LIBRARY_ROOT=/srv/library`）。
当前已实现的覆盖键为子集：`ROXY_SERVER_HOST` / `ROXY_SERVER_PORT` /
`ROXY_DATA_DIR` / `ROXY_AUTH_MODE` / `ROXY_MEDIA_LIBRARY_ROOT` /
`ROXY_MEDIA_LINK_MODE` / `ROXY_POLICY_AUTO_APPROVE_THRESHOLD` /
`ROXY_POLICY_MULTI_VERSION` / `ROXY_SCANNER_RESCAN_INTERVAL`，
以 `internal/config/config.go` 的 `applyEnv` 为准。

## 14. REST API 端点清单

前缀 `/api`；除 login 外均需会话认证；`/api/events` 为 SSE。

**实现状态**：M0 已实现认证四端点（login/logout/me/credentials）与
`/api/health`；其余为规划目标，按 ROADMAP 里程碑逐步交付（实现以
`internal/api/` 代码为准）。

```
认证
  POST /api/auth/login                 登录
  POST /api/auth/logout                登出
  GET  /api/auth/me                    当前用户
  PUT  /api/auth/credentials           改用户名/密码

源管理
  GET|POST   /api/sources              列表/新增
  PUT|DELETE /api/sources/{id}         修改/删除
  POST       /api/sources/{id}/scan    手动触发扫描
  GET        /api/sources/{id}/files   文件清单（含解析结果；M2 增补，验收项"解析结果展示"）

索引（M2 增补，D-022 的运维入口）
  GET  /api/index                      本地 Bangumi 索引状态（dump 版本/导入时间/条目计数/进行中任务）
  POST /api/index/refresh              触发导入/刷新；body 可选 {local_path}（缺省应用内下载）

审核队列
  GET  /api/review?state=…             工单列表
  POST /api/review/{id}/approve        批准
  POST /api/review/{id}/reject         驳回
  POST /api/review/{id}/rework         附提示返工 {hint}
  POST /api/review/{id}/assign         改派 {series_id | query, slot}

系列
  GET  /api/series                     列表（分页/搜索）
  GET  /api/series/{id}                详情（季/集/版本树、策略、笔记）
  PUT  /api/series/{id}/policy         系列级策略覆盖
  POST /api/series/{id}/rematch        整系列重新匹配
  POST /api/series/{id}/reapply-policy 按当前策略重算 → 返回 diff（不落盘）
  POST /api/series/{id}/apply-diff     确认应用 diff

决策与锁
  GET  /api/series/{id}/placements     决策列表
  PUT  /api/placements/{id}            手动指定槽位（自动打 manual_lock）
  POST /api/placements/{id}/lock       加锁
  POST /api/placements/{id}/unlock     解锁

台账
  GET /api/library/tree                库目录树（含链接→源映射）
  GET /api/ledger?broken=1             台账/悬空链接清单

反馈
  GET|POST /api/feedback               列表/新增
  DELETE   /api/feedback/{id}          删除

任务
  GET  /api/tasks                      任务列表
  GET  /api/tasks/{id}                 任务详情
  POST /api/tasks/{id}/cancel          取消

LLM 日志
  GET /api/llm-logs                    列表（按 task/时间过滤）
  GET /api/llm-logs/{id}               完整请求/响应

设置与概览
  GET|PUT /api/settings                运行时设置（阈值、语言映射等）
  GET     /api/dashboard               仪表盘统计

实时
  GET /api/events                      SSE：任务进度、审核队列变化、漂移告警
```

## 15. 仓库结构

见 `AGENTS.md` 的目录结构一节。要点：

- `cmd/roxy` 入口；业务代码全部在 `internal/`（不对外暴露包）。
- `web/` 前端构建产物经 `go:embed` 内嵌，交付单二进制。
- `testdata/` 仅存纯文本/KB 级假文件（解析 fixture、NFO 黄金样本）；
  真实媒体一律在 `test_share/`（不入库）。
- `deployments/`：podman 测试脚本（v1）→ quadlet/compose 模板（M6）。
- `flake.nix` 是唯一构建定义（devShell / packages.default / OCI 镜像）。
