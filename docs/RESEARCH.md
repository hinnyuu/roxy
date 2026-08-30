# roxy 外部调研存档

> 规则：新增外部 API 前先在本文件登记调研结论（见 AGENTS.md）。
> 标注：`[已验证]` = 已读官方文档/规范（附日期）；`[待验证]` = 实现时需实测。

---

## 1. Bangumi API（bgm.tv）

**状态：[已验证] 2026-08-29（官方仓库 bangumi/api 文档 + OpenAPI v0 规范）**

- 基地址：`https://api.bgm.tv`；文档：`https://bangumi.github.io/api/`
  （Swagger UI，内容与仓库 `open-api/v0.yaml` 相同）。
- **认证模型**（OpenAPI securitySchemes）：
  - 读接口（搜索、条目、章节、角色、人物、关联）为 `OptionalHTTPBearer`——
    **匿名可调用**，仅限流更严、看不到 NSFW 与私有收藏。
  - 写接口（收藏修改等）为 OAuth2 authorization code flow
    （`/oauth/authorize` → `/oauth/access_token`，token 7 天有效，
    refresh_token 可续期），需注册应用取得 client_id/secret。
  - **无个人令牌（personal access token）页**：官方 How-to-Auth 文档仅描述
    OAuth2；用户（账号持有者）2026-08-29 实测登录态访问
    `https://bgm.tv/settings/auth` 不存在。→ 决策见 DECISIONS D-023。
- **User-Agent 强制要求**：非浏览器调用必须带含开发者 ID 与应用名的 UA，
  默认请求库 UA 可能被禁。开源项目需附项目主页。格式示例：
  `RyougiShiki-214/roxy (https://github.com/hinnyuu/roxy)`。
  反面示例（官方点名禁止）：`database`、`Bangumi/1.0`。
- roxy 使用的端点（v0）：
  - `POST /v0/search/subjects`：关键词搜索；过滤条件含 `type`（2=动画）、
    `air_date`（`>=YYYY-MM-DD` 形式）、`tag`、`rating`、`rank`、`nsfw`；
    排序 `match|heat|rank|score`。**标注为实验性 API，行为可能变动。**
  - `GET /v0/subjects/{id}`：条目详情（缓存 300s）。
  - `GET /v0/subjects/{id}/subjects`：条目关联（续作/剧场版/衍生）。
  - `GET /v0/episodes?subject_id=…&type=…`：章节列表（分页 ≤200/页）。
  - `GET /v0/subjects/{id}/image?type=…`：封面（302 跳转；无图回默认图）。
- 限流：匿名具体阈值未在文档量化；策略 = 本地索引为主、在线调用最小化
  （见 D-022）。

## 2. bangumi/Archive（官方全量数据 dump）

**状态：[已验证] 2026-08-29（仓库 README）**

- 仓库：`https://github.com/bangumi/Archive`；定位：官方定期导出 wiki 数据，
  明确鼓励用于"不需要实时数据的场景"以减少爬虫。
- **更新周期：每周三凌晨 05:00（GMT+8）**。
- 下载：GitHub Releases（tag `archive`）；最新文件地址解析
  `aux/latest.json`（上传后更新）。
- 导出内容（与 roxy 相关性标注）：
  - **条目 Subject**：`id`、`type`（**2=动画**）、`name`、`name_cn`、
    `infobox`（原始 wiki 字符串）、`platform`（TV/剧场版/OVA 等）、
    `summary`、`nsfw`、`date`、`tags`、`score`、`rank`、`meta_tags`。
  - **章节 Episode**：`id`、`subject_id`、`name`、`name_cn`、`sort`（集数）、
    `airdate`、`duration`、`disc`、**`type`：0 正篇 / 1 特别篇 / 2 OP /
    3 ED / 4 Trailer / 5 MAD / 6 其他**——与 roxy 槽位分类同构。
  - **条目关联**：`subject_id`、`related_subject_id`、`relation_type`、`order`。
  - 人物/角色及其关联（v1 不导入）。
- 常量对应关系（relation/platform/staff）：`github.com/bangumi/common`（yaml）。
- infobox 解析（如需制作组等字段）：官方 Go 解析器
  `github.com/bangumi/wiki-parser-go`（语法规范 `bangumi/wiki-syntax-spec`）。
- **[待验证]（M2）**：dump 实际体积（压缩/解压）、文件格式与分片方式、
  导入耗时。磁盘预算：宿主机 3TB SSD，无约束。

## 3. AniList API v2

**状态：[已验证] 2026-08-29（官方 gitbook 文档）**

- 端点：单一 GraphQL 端点 `POST https://graphql.anilist.co`。
- 文档：`https://anilist.gitbook.io/anilist-apiv2-docs/`（旧地址
  `anilist-graphql-api-docs` 已迁移）。
- **认证：读取类查询无需 token**（公开 API）；OAuth 仅用于用户数据与写操作。
- **限流**：
  - 常规 **90 请求/分钟**；**当前处于官方标注的降级状态：30 请求/分钟**
    （"temporary measure until the API is fully restored"）。
  - 另有突发限制器；超限返回 429 + `Retry-After` / `X-RateLimit-Reset`；
    响应头带 `X-RateLimit-Limit` / `X-RateLimit-Remaining`。
  - 官方不接受限流提升申请（当前暂停）；过量请求可能导致 IP 被手动封禁。
  - → 设计约束：**必须批量查询（一次 GraphQL 打包多需求）+ 结果缓存**。
- 可用性风险：严重故障时可能临时关停 API（403 + 公告于官方 Discord）。
- roxy 使用的字段（`Media`）：`id`、`idMal`、`title{native,romaji,english}`、
  `synonyms`（别名）、`format`（TV/MOVIE/OVA/ONA/SPECIAL）、`season`、
  `seasonYear`、`episodes`、`status`、`relations`、`externalLinks`、
  `airingSchedule`、`coverImage`、`bannerImage`。
- 内容注意：条目可能含成人内容（`isAdult` 过滤）。

## 4. TMDB

**状态：[已知] 通用事实，实现时复核条款**

- 免费 API key；速率宽松（官方约 40–50 req/s 量级，以条款为准）。
- 用途：海报/背景图（图床 CDN，**下载到本地**存放）、`external_ids`
  （imdb_id 桥接）、电影条目补充。
- 动漫剧集数据质量参差，**不作为动漫元数据主力**（主力为 Bangumi）。
- 署名要求：展示数据需署名 TMDB（WebUI 关于页处理）。

## 5. 明确排除的源

| 源 | 结论 | 理由 |
|---|---|---|
| TVDB | 不接入（架构留槽位） | API 转向订阅制/项目审批，接入门槛与持续性风险高 |
| IMDB | 不直连 | 无官方 API；imdb_id 经 TMDB `external_ids` 获取 |
| 通用搜索引擎 | 仅可选扩展，默认关闭 | 结构化"准搜索"（Bangumi/AniList/预热缓存）已覆盖主场景；避免额外 key 与 ToS 问题 |

## 6. LLM 供应商调研

**状态：[已验证] 2026-08-29（DeepSeek 官方文档）；其余为实现时实测**

- **DeepSeek**（`https://api.deepseek.com/v1`，Chat Completions）：
  - 现行文档模型：`deepseek-v4-flash`、`deepseek-v4-pro`（含视觉实验版）。
  - 支持：`response_format: json_object`、tools/function calling、
    思考模式（`thinking.type`、`reasoning_effort`）、流式。
  - **未暴露联网搜索参数**（旧版曾有 `enable_search`，现行文档已无）
    → 支撑决策 D-019：不依赖供应商原生搜索。
- **Qwen / DashScope 兼容模式**（Alibaba Token Plan）：
  - 端点 `https://dashscope.aliyuncs.com/compatible-mode/v1`。
  - 联网搜索：历史上兼容模式支持 `enable_search` 请求参数；
    **[待验证]（M3）**：参数现状、返回中引用的结构化程度、计费。
- **通用结论**：以 Chat Completions 为基座（D-016）；每 provider 声明
  `native_search` 能力位，实测后开启。

## 7. 媒体服务器行为（M1 实测结论，2026-08-30）

**状态：[已验证] 除标注外**，原始记录见 `docs/testing/m1.md` §6。

| # | 验证项 | Jellyfin 10.11.11 | Emby 4.9.5 |
|---|---|---|---|
| 1 | `Season 00` / `Specials` 目录识别 | 均识别为特别篇 | 同左 |
| 2 | 同集多文件合并为多版本 | **不合并：无任何合并路径**（M1.5 三变体全败 → 版本仓库 D-038/D-039） | 合并为多版本 |
| 3 | 多版本字幕 sidecar 按 basename 跟随 | 延至 M4（假文件无法触发播放） | 同左 |
| 4 | `S01E01E02` 多集合一 | 单一条目 | 单一条目 |
| 5 | `Extras/` 与 `-trailer` 识别 | `Extras/`=花絮；根目录 `-trailer` 忽略 | **根目录 `-trailer` 收为第一季剧集（红线 D-037）**；`Extras/` 并入特别篇 |
| 6 | 禁用刮削后库目录零写入 | 零写入 | 零写入 |
| 7 | NFO `<uniqueid>` 于"识别"页 | 不可见（非阻塞，D-036） | 不可见（同左） |
| 8 | 相对软链接的读取/字幕加载 | **待 M4**（M1 用实体假文件，未测链接） | 同左 |
| 9 | 中文目录/文件名（全角字符、`&`、`~`、「」） | 正常 | 正常 |

## 8. 参考生态（不做依赖，仅供借鉴）

- MoviePilot / tinyMediaManager / Shoko / filebot：既有整理器的经验教训
  （roxy 的差异化：LLM-first、软链接零破坏、NFO 中心、人工闭环）。
- `bangumi/wiki-parser-go`：infobox 解析（需要时引入）。
