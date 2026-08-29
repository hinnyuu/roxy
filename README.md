# roxy

> **roxy** is an LLM-agent-driven anime media library organizer for
> Jellyfin / Emby / Kodi — NFO-first metadata, symlink-based zero-touch
> organization, BT-seeding safe.

roxy 是面向 Jellyfin / Emby / Kodi 的**动漫媒体库整理器**：以 LLM 为核心识别引擎，
以 Bangumi / AniList / TMDB 为事实来源做交叉验证，产出 NFO 元数据与软链接媒体库，
对 BT 下载源**零破坏**。

---

## 解决什么问题

家用媒体服务器处理动漫番剧有两大顽疾：

**➀ 刮削错乱。** 欧美剧集的"季→集"假设在动漫面前全面失效：TV 版、剧场版、OVA、
PV、CM、NCOP、NCED 混杂一目录；第 0 集、第 12.5 集、FINAL 集等不规则命名层出不穷。
内置刮削器对此错误率极高。

**➁ 整理即破坏。** 原地改名会破坏 BT 做种；复制后改名让空间翻倍；硬链接无法跨卷，
且与 docker/podman 多容器场景冲突。

roxy 的答案：

- **禁用服务器内置刮削，改用 roxy 生成的 NFO**——媒体服务器退化为纯展示层，
  元数据正确性由 roxy 全权负责。
- **多容器共享挂载卷 + 软链接**：BT 下载目录仅下载器可写（其余容器只读），
  轻量媒体库目录仅 roxy 可写（服务器只读）。媒体库里只有软链接、NFO、封面与字幕，
  源文件永不被触碰，做种永不中断，空间零膨胀。

## 核心特性（规划）

- **LLM 智能识别**：规则引擎先行（零成本覆盖 80% 结构化命名），LLM 兜底歧义样本；
  所有 LLM 决策强制携带证据并经 API 交叉验证，无证据即无效。
- **多源元数据**：Bangumi（中文动漫元数据主力）+ AniList（新番与别名）+
  TMDB（图片与 ID 桥接）；Bangumi 官方周更数据 dump 构建**本地离线索引**，
  候选检索零限流。
- **人工审核闭环**：高置信自动放行，低置信进入 WebUI 审核队列；支持驳回、改派、
  附提示返工；人工纠正沉淀为规则，同一错误不犯第二次。
- **返工与策略覆盖**：全局策略 → 系列级覆盖 → 单文件手动指定的三级模型，
  任何归属错误可经台账精确回滚并重算，变更先出 diff 预览再落盘。
- **版本并存**：同一集的多个压制版本（及各自配套的字幕版本）自动识别、并存、
  正确配对。
- **零破坏 + 台账**：一切产出物登记入账，可审计、可回滚；定期对账检测悬空链接。

## 工作原理（概览）

```
监控目录 / 下载客户端事件
        │
        ▼
  Scanner（源发现）──► Parser（规则引擎 + LLM 兜底）
        │                        │
        ▼                        ▼
  本地 Bangumi 索引 / AniList / TMDB 检索候选
        │                        │
        ▼                        ▼
  Matcher（LLM 带证据决策 ──► 验证器交叉校验 ──► 置信度分级）
        │                        │
        ▼                        ▼
  高置信自动放行            低置信 ──► WebUI 人工审核 / 返工
        │                        │
        └────────┬───────────────┘
                 ▼
  Organizer（相对软链接 + NFO + 封面 + 字幕配对，全部记入台账）
                 │
                 ▼
  /media/library（Jellyfin / Emby / Kodi 只读扫描，NFO 直读）
```

完整架构见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 项目状态

当前阶段与下一步动作见 [docs/ROADMAP.md](docs/ROADMAP.md) 的**"当前进度"**节
（项目状态单一事实源，随里程碑实时更新）。全部设计决策见
[docs/DECISIONS.md](docs/DECISIONS.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [AGENTS.md](AGENTS.md) | 项目工作规范（面向 AI agent 与协作者） |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 完整架构、数据模型、schema、配置、API |
| [docs/DECISIONS.md](docs/DECISIONS.md) | 全部设计决策记录（ADR） |
| [docs/ROADMAP.md](docs/ROADMAP.md) | 里程碑 M0–M6 与宿主机测试计划 |
| [docs/RESEARCH.md](docs/RESEARCH.md) | 外部 API 调研存档 |
| [docs/testing/](docs/testing/) | 测试分工与检查单 |

## 许可证

[GNU Affero General Public License v3.0](LICENSE)
