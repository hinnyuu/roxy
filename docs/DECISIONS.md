# roxy 决策记录（ADR）

> 规则：任何改变设计决策的变更，**先修本文件再改代码**（见 AGENTS.md 工作流约定）。
> 状态：`accepted` 已定案；`draft` 草案待实测；`superseded` 被后续决策取代。
> 所有决策均出自 2026-08 的五轮设计讨论。
>
> 维护规则：编号（D-xxx）只增不复用；被废弃的决策保留原文、状态改为
> `superseded` 并注明取代它的新决策编号，**不得删除或改写历史条目**。

---

## D-001 主力目标服务器：Jellyfin，Emby/Kodi 次选

- 状态：accepted
- 决策：以 Jellyfin（实测版本 10.11.11）为第一适配目标；Emby 4.9.5 同步实测；
  Kodi 暂缓（无 SMB 场景，见 D-004）。
- 理由：用户环境现状；Jellyfin 的解析行为决定命名规范保守程度（见 D-005）。

## D-002 NFO-first：禁用服务器内置刮削

- 状态：accepted
- 决策：媒体服务器禁用全部在线刮削器，仅启用 NFO 读取；元数据正确性由 roxy
  全权负责；服务器退化为纯展示层。
- 理由：动漫复杂构成（TV/剧场版/OVA/PV/CM/NCOP/NCED）与不规则集号
  （第 0 集、12.5 集、FINAL 集）使内置刮削错误率极高；NFO 是三家服务器
  共同支持的稳定接口。

## D-003 整理方式：软链接，排除改名/复制/硬链接

- 状态：accepted
- 决策：媒体库仅由软链接 + 小文件（NFO/图片）构成；源文件永不被触碰。
- 理由：原地改名破坏 BT 做种；复制使空间翻倍；硬链接无法跨卷且与
  docker/podman 挂载模型冲突；软链接在共享挂载卷内是零破坏、零膨胀、
  可任意命名的唯一解。

## D-004 部署形态：单一公共父目录；排除 SMB 网络共享

- 状态：accepted
- 决策：官方部署形态为"下载目录与媒体库同处一个公共父目录，所有容器以相同
  结构挂载"。明确不支持 SMB/NFS 共享给 Kodi 等跨机器场景。
- 理由：相对软链接依赖兄弟关系保持一致（D-026）；SMB 下符号链接无法跨机器
  解析，属不可修复的限制，直接排除而非半吊子支持。

## D-005 命名保守化：文件系统层遵守 S01E01 体系，自由度全部放 NFO

- 状态：accepted（具体模板待 M1 冻结，见 D-027）
- 背景：曾考虑按 Bangumi 体系"狂放"命名媒体库目录。
- 决策：目录名用 `Season 00/01…`，链接名用 `S01E01 - {集标题}`；中文标题、
  绝对序号、放送日期等全部写入 NFO。**文件名与 NFO 由同一 placement 决策
  生成，永远一致。**
- 理由：① Jellyfin/Emby 的文件解析器先于 NFO 解析运行，文件必须先被硬编码
  正则"识别为剧集"；② NFO 的 season/episode 覆盖行为在 Jellyfin 各版本
  不一致，Kodi 独有的 displayseason/displayepisode 另两家不支持；
  ③ 软链接改名零成本，没有理由赌解析器行为；④ 文件名与 NFO 一致可彻底
  消除"谁优先"的歧义。

## D-006 不规则集号归位：第 0 集/12.5 集/FINAL 集 → Season 00 特别篇

- 状态：accepted
- 决策：不规则编号一律映射为 S00 特别篇体系，NFO 中保留原始语义说明。
- 理由：Bangumi ep_type=1（特别篇）与 TVDB S00 体系本就收录这些内容；
  文件名保持机器可读，语义由 NFO 承载（D-005 的推论）。

## D-007 系列是第一公民：逻辑归并，禁止物理预分拣

- 状态：accepted
- 背景：第三季、剧场版、字幕往往在不同时间、不同目录到达，需收敛进同一
  系列媒体库目录。曾考虑增加"物理预分拣层"。
- 决策：Series 实体（= Bangumi subject）为收敛点；任何文件无论何时、来自
  哪个源，经匹配挂到同一 Series 即归入同一输出目录。绝不移动/复制源文件。
- 理由：物理分拣违反零破坏原则且无必要；逻辑归并天然解决"晚到资源"问题。

## D-008 三级策略覆盖：全局 → 系列级 → 单文件手动

- 状态：accepted
- 决策：归属策略 = 单文件手动指定 > 系列级覆盖（series.policy_overrides）
  > 全局默认（config.policy）。默认：剧场版独立电影条目、OVA 独立目录、
  NCOP/NCED 进 S00、PV/CM 进 Extras。
- 理由：默认策略覆盖大多数番剧；特殊番剧（如"带集数 OVA 也要进 S00"）
  用系列级覆盖；个别文件用手动指定。三级模型保证任何归属错误都有纠正入口。

## D-009 策略变更必须 diff 预览后落盘

- 状态：accepted
- 决策："重新应用策略"先生成变更 diff（将移动/改名/删除的产出物清单），
  用户确认后才执行。
- 理由：建立信任的关键交互；产出物全是软链接+小文件，重算廉价，没有理由
  跳过预览。

## D-010 返工 = 台账精确回滚 + 重新提案

- 状态：accepted
- 决策：所有产出物登记 ledger 表；返工按台账删除对应产出物后重新进入流水线。
  manual_lock 项不受自动重算影响。
- 理由：台账使回滚成为精确、可审计的操作；软链接+小文件的产出构成使返工
  成本趋近于零。

## D-011 版本并存：同集多压制版共存，单版本无后缀

- 状态：accepted
- 决策：同集多版本全部保留；链接名仅在存在 ≥2 版本时给全体版本加
  ` [{version}]` 后缀；第二个版本到达时台账驱动地给既有版本补后缀。
- 理由：用户明确要并存（不同压制版各有配套字幕）；单版本场景保持命名整洁。
- 待验证：Jellyfin/Emby 对同集多文件的合并行为（M1 实测）；若不符合预期，
  兜底方案"主版本进库 + 其余进库外版本仓库（可随时提拔）"已获预认可。

## D-012 version_key = 原始中括号归一化，配对不依赖语义分类

- 状态：accepted
- 背景：`[Nekomoe kissaten&VCB-Studio] …` 这类"字幕组&压制组同括号"、
  多组合作等命名无法靠正则做语义分类。
- 决策：配对层用原始中括号字符串归一化后作 version_key（确定性，零 LLM）；
  LLM 只做显示层的语义标注（谁是字幕组/压制组），结果缓存、可纠错、错了
  不影响配对。
- 理由：视频与配套字幕必然共享相同中括号/文件名前缀，原始字符串相等已是
  充分的配对信号；把"会出错的部分"隔离到无关紧要的显示层。

## D-013 字幕配对降级链与语言标签映射

- 状态：accepted
- 决策：配对优先级：① 字幕 basename == 视频 basename + 语言标签（精确）
  → ② 同 version_key + 同集 → ③ 只定到集：挂所有版本 + 审核提示
  → ④ 人工。`JPSC→zh-CN`、`JPTC→zh-TW` 等惯例标签映射表可编辑。
- 理由：字幕是源资源的一部分，roxy 只整理不下载（D-024）。

## D-014 置信度分级：高置信自动放行，低置信人工审核

- 状态：accepted
- 决策：置信度 = 标题匹配分 + API 证据一致分 + 规则命中强度 + LLM 自报置信度
  的加权；≥ 0.90（可配）自动放行，其余进人工队列。
- 理由：用户选择半自动模式；可解释的分解便于人工快速判断。

## D-015 反馈回流：纠正沉淀为 FeedbackNote

- 状态：accepted
- 决策：用户每次纠正落为 global/series/pattern 三种作用域的笔记，注入后续
  prompt 或规则引擎；同一错误不犯第二次。
- 理由：人工审核的价值必须累积，否则审核队列永远是纯成本。

## D-016 LLM 接入：OpenAI 兼容 Chat Completions 为唯一基座

- 状态：accepted
- 决策：统一 `/v1/chat/completions`；不使用 OpenAI Responses API；
  多 provider 按 priority 降级；结构化输出三级降级
  （json_schema → json_object → prompt+重试）。
- 理由：Chat Completions 是 Qwen/DeepSeek/OpenRouter/Ollama 等的最大公约数；
  Responses 是私有接口，收益不抵供应商锁定。

## D-017 不内嵌外部 agent CLI（如 opencode）

- 状态：accepted
- 决策：roxy 的"agent"= 确定性流水线 + 少数 LLM 决策点 + 验证器 + 人工队列；
  不套壳任何通用 agent CLI。远期可将 roxy 自身暴露为 MCP server 供外部
  agent 驾驶（不设时间表）。
- 理由：延迟/成本/输出不确定性不可控；外部 CLI 的工具面与本任务不匹配；
  产品不应耦合外部工具的版本行为。

## D-018 LLM 输出无证据即无效

- 状态：accepted
- 决策：三个决策点（parse/match/mapping）的 schema 均强制 evidence 字段
  （引用的 API 条目与字段）；验证器回查 API 交叉校验后才采信。
- 理由：防幻觉的结构性手段，优于提示词恳求。

## D-019 搜索三层设计：搜索是 roxy 的能力，不是模型的能力

- 状态：accepted
- 决策：① 原生联网搜索仅作可选加速器（provider 能力位）；② 通用兜底为
  roxy 自检索 + 结果注入 prompt（模型只推理）；③ 铁律：搜索只产候选，
  最终匹配必须直连元数据 API 验证。搜索结果缓存入库。
- 理由：供应商联网能力参差不齐（DeepSeek 现行 API 已无联网搜索参数）；
  自检索使证据可结构化留存、可审计；动漫鉴别场景下结构化"准搜索"
  （Bangumi/AniList/新番预热缓存）比通用搜索引擎更有效。

## D-020 新番预热缓存

- 状态：accepted
- 决策：每季自动缓存当季/下季番列表（Bangumi 日历 + AniList 季度查询：
  标题、别名、年份、ID），新番鉴别优先本地命中。
- 理由：新番是鉴别难点的高发区；预热后多数情况无需搜索与在线调用。

## D-021 元数据源分工：Bangumi 主力 + AniList + TMDB；不接 TVDB/IMDB

- 状态：accepted
- 决策：Bangumi 提供中文元数据与集分类（主力）；AniList 提供新番/别名/
  交叉验证；TMDB 提供图片与 ID 桥接（imdb_id 仅经此获取）。不接 TVDB
  （API 订阅制）；不直连 IMDB（无官方 API）。架构保留适配器槽位。
- 理由：各源比较优势清晰；避开收费与无 API 的坑。

## D-022 bangumi/Archive 周更 dump → 本地 SQLite FTS 索引

- 状态：accepted
- 决策：每周拉取官方全量 dump（动画条目+章节+关联），导入本地
  `bgm_subjects/bgm_episodes/bgm_relations` + FTS5；候选检索本地化；
  在线 Bangumi API 仅用于增量、图片与最终核验。
- 理由：消灭匿名限流问题、支持离线、检索免费；dump 的章节 type 枚举
  （0正篇/1特别篇/2OP/3ED/4Trailer/5MAD/6其他）与 roxy 槽位分类同构。
- 代价：磁盘约 1–2GB（宿主机 3TB SSD，可接受）；实际体积 M2 验证。

## D-023 Bangumi 认证：匿名 + 规范 UA 起步，OAuth2 延后

- 状态：accepted
- 决策：M2 起匿名调用读接口（满足需求），强制规范 User-Agent
  （`RyougiShiki-214/roxy (https://github.com/hinnyuu/roxy)`）；
  OAuth2 授权码流程排 M5/M6，仅"同步 Bangumi 收藏"等写功能需要。
- 背景：经查证官方无个人令牌页（用户实测 settings/auth 不存在），
  官方认证机制仅 OAuth2。

## D-024 字幕：只整理，不下载

- 状态：accepted
- 决策：字幕是源资源，由用户自行下载；roxy 对字幕目录只读，仅做配对与
  软链接。不集成任何字幕下载器。
- 理由：收敛项目范围；字幕下载有成熟独立工具。

## D-025 源发现：v1 监控目录 + WebUI 手动触发，v2 下载客户端集成

- 状态：accepted
- 决策：SourceProvider 接口从第一天存在；v1 实现 DirScanProvider（手动触发
  + 可选定时），v2 增加 qBittorrent WebAPI 与 Transmission RPC。
  sources 表预留 provider_type/provider_config，source_files 表预留
  provenance（torrent 元数据）。
- 理由：先跑通最短路径；接口先行保证 v2 纯增量。

## D-026 相对软链接优先，绝对路径 + 映射仅兜底

- 状态：accepted
- 决策：`link_mode: relative` 默认；`filepath.Rel` 自动计算；
  `absolute + path_mappings` 保留为兜底代码路径。
- 理由：相对链接按链接位置解析、与挂载点无关，在单一公共父目录形态下
  天然成立，直接消灭路径映射配置负担。

## D-027 命名规范冻结流程：M1 兼容性实测后冻结

- 状态：accepted（规范本体为 draft）
- 决策：文件系统命名模板以草案状态写入 ARCHITECTURE.md §4；M1 用边界用例
  假库在 Jellyfin/Emby 实测（S00、多版本、多集合一、Extras、字幕拾取），
  按实测结果冻结。冻结前任何命名变更须重列实测清单。
- 理由：服务器解析行为无法从文档完全推断，用实验代替争论。

## D-028 技术栈：Go 后端 + React/TS 前端 + SQLite

- 状态：accepted
- 决策：后端 Go（静态单二进制、fsnotify、纯 Go SQLite 驱动）；前端
  React + TypeScript + Vite，构建产物 go:embed 内嵌；状态存储 SQLite。
- 理由：单二进制 + 极简镜像与 Nix dockerTools 构建链配合最佳；文件密集
  型负载适合 Go；前端交互复杂度需要成熟组件生态。

## D-029 认证：admin/admin 起步 + 三重护栏 + 可升级架构

- 状态：accepted
- 决策：会话认证（httpOnly cookie + bcrypt）；初始 admin/admin
  （ROXY_ADMIN_PASSWORD 可覆盖）；未改默认密码时 WebUI 常驻警告条 +
  启动日志警告；测试期端口仅绑 127.0.0.1；schema 预留 role 字段。
- 理由：前期迭代零摩擦与未来安全不冲突；绑定回环比强密码更有效。

## D-030 交付：GitHub Actions + ghcr.io，flake.nix 是唯一构建定义

- 状态：accepted
- 决策：CI 用 Nix（GH runner 上 nix-installer action）执行与本地相同的
  `nix build .#image`（dockerTools），skopeo/crane 推送
  `ghcr.io/hinnyuu/roxy`；main 合并推 `:dev`，打 tag 推版本。
  **不维护 Dockerfile 第二套构建定义。**
- 理由：单一构建源避免漂移；字节级可复现；开发容器内无嵌套 docker，
  dockerTools 是本地可验证的同一条路径。

## D-031 测试分工：容器内单测/golden，宿主机集成

- 状态：accepted
- 决策：开发容器内做单元测试、golden 测试（解析 fixture、NFO 黄金样本、
  软链接布局模拟）、API 测试；真实媒体服务器集成测试由用户在宿主机
  podman 执行，按 docs/ROADMAP.md 的检查单回填结果。交付双轨：
  二进制直跑（快速迭代）+ OCI 镜像（集成测试）。
- 理由：开发容器无嵌套能力；服务器行为假设必须实测（D-027）。

## D-032 许可证：AGPL-3.0

- 状态：accepted
- 决策：采用 GNU Affero General Public License v3.0（建仓时由用户选定）。
- 备注：自用为主、开源分发；AGPL 的网络使用条款对自托管工具无额外负担。

## D-033 test_share 位置：仓库内、gitignore、用户管理

- 状态：accepted
- 决策：宿主机测试目录为仓库内 `test_share/`（`/data/projects/hinnyuu/roxy/test_share`），
  已列入 .gitignore；任何情况下不得 `git add -f`。
- 理由：测试拓扑（podman 挂载）与仓库同址最方便；媒体文件体积决定必须
  与 git 隔离。
