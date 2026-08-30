// Package domain 定义核心实体与状态机（docs/ARCHITECTURE.md §5）。
// schema 唯一事实源是 internal/db/migrations/（AGENTS.md），本包结构体与其逐列对齐。
package domain

import "time"

// Now 返回落库用的时间戳文本（UTC RFC3339，与 auth 包既有约定一致）。
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Source 监控根（sources 表）。
type Source struct {
	ID             int64
	Name           string
	Path           string
	Kind           string // mixed | video | subtitle
	ProviderType   string // dirscan | qbittorrent | transmission
	ProviderConfig string // JSON，v2 客户端连接参数
	Enabled        bool
	CreatedAt      string
}

// SourceFile 扫描到的文件（source_files 表）。
type SourceFile struct {
	ID          int64
	SourceID    int64
	AbsPath     string
	Size        int64
	MTime       string
	Kind        string // unknown | video | subtitle | nfo | image | other
	Status      string // 见 SourceFileStatus*
	ParseResult string // JSON（domain.ParseResult）
	Provenance  string // JSON，v2 torrent 元数据
	CreatedAt   string
	UpdatedAt   string
}

// Series 系列 = Bangumi subject（series 表）。
type Series struct {
	ID              int64
	BgmSubjectID    int64
	AnilistID       int64 // 0 = 无
	TMDBID          string
	IMDBID          string
	Title           string
	TitleOriginal   string
	Year            int    // 0 = 未知
	SeriesType      string // tv | ova | ona | movie | special | other
	ParentSeriesID  int64  // 0 = 无
	LibraryKind     string // tv | movie
	LibraryPath     string
	PosterPath      string
	FanartPath      string
	PolicyOverrides string // JSON，§10.1 覆盖
	Status          string // active | archived
	CreatedAt       string
	UpdatedAt       string
}

// Placement 决策：某 SourceFile → 某 Series 槽位（placements 表）。
type Placement struct {
	ID                    int64
	SourceFileID          int64
	SeriesID              int64
	SlotType              string // 见 SlotType*
	Season                int
	SeasonValid           bool
	Episode               float64
	EpisodeValid          bool
	EpisodeEnd            float64 // 多集合一文件的结束集（迁移 0003）
	EpisodeEndValid       bool
	EpisodeTitle          string
	VersionKey            string
	VersionLabel          string
	Vault                 bool // 1=版本仓库内（D-039）
	SubtitleOfPlacementID int64
	Confidence            float64
	DecisionSource        string // rule | llm | human
	Evidence              string // JSON
	ReviewState           string // 见 PlacementReviewState*
	ManualLock            bool
	CreatedAt             string
	UpdatedAt             string
}

// LedgerEntry 物料台账（ledger 表）。
type LedgerEntry struct {
	ID          int64
	PlacementID int64
	Artifact    string // symlink | nfo | image | dir
	Path        string
	Target      string
	State       string // active | removed
	CreatedAt   string
}

// ReviewCase 审核工单（review_cases 表）。
type ReviewCase struct {
	ID          int64
	PlacementID int64
	Reason      string
	LLMLogID    int64 // 0 = 无
	UserNote    string
	State       string // open | approved | rejected | reworked
	CreatedAt   string
	ResolvedAt  string
}

// FeedbackNote 用户纠正/建议（feedback_notes 表）。
type FeedbackNote struct {
	ID         int64
	Scope      string // global | series | pattern
	SeriesID   int64
	Pattern    string
	Note       string
	InjectInto string // prompt | rule | both
	CreatedAt  string
}

// Task 异步任务（tasks 表）。
type Task struct {
	ID         int64
	Kind       string // scan | match | materialize | rework | reconcile | index_refresh
	Payload    string // JSON
	State      string // 见 TaskState*
	Progress   string // JSON
	Error      string
	CreatedAt  string
	StartedAt  string
	FinishedAt string
}

// BgmSubject 本地索引条目（bgm_subjects 表）。
type BgmSubject struct {
	ID       int64
	Type     int // 2=动画
	Name     string
	NameCn   string
	Platform string
	Date     string
	Score    float64
	Rank     int
	NSFW     bool
	Summary  string
}

// BgmEpisode 本地索引章节（bgm_episodes 表）。
type BgmEpisode struct {
	ID        int64
	SubjectID int64
	Name      string
	NameCn    string
	Sort      float64
	EPType    int // 0正篇 1特别篇 2OP 3ED 4Trailer 5MAD 6其他
	Airdate   string
}

// BgmRelation 条目关联（bgm_relations 表）。
type BgmRelation struct {
	SubjectID        int64
	RelatedSubjectID int64
	RelationType     string
}

// SourceEvent 触发归一化（ARCHITECTURE.md §3 原则 1）：手动扫描、定时扫描
// 与未来下载完成事件都归一为该事件进入同一流水线。
type SourceEvent struct {
	SourceID int64
	AbsPath  string
	Op       string // upsert | remove
}

const (
	SourceFileNew     = "new"
	SourceFileParsing = "parsing"
	SourceFileParsed  = "parsed"
	SourceFilePlaced  = "placed"
	SourceFileIgnored = "ignored"
	SourceFileError   = "error"
)

const (
	PlacementProposed      = "proposed"
	PlacementAutoApproved  = "auto_approved"
	PlacementPendingReview = "pending_review"
	PlacementApproved      = "approved"
	PlacementRejected      = "rejected"
	PlacementRework        = "rework"
)

const (
	SlotEpisode = "episode"
	SlotSpecial = "special"
	SlotMovie   = "movie"
	SlotOP      = "op"
	SlotED      = "ed"
	SlotPV      = "pv"
	SlotCM      = "cm"
	SlotExtra   = "extra"
	SlotSub     = "subtitle"
	SlotIgnored = "ignored"
)

const (
	DecisionRule  = "rule"
	DecisionLLM   = "llm"
	DecisionHuman = "human"
)

const (
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

const (
	ReviewOpen     = "open"
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
	ReviewReworked = "reworked"
)

const (
	EventUpsert = "upsert"
	EventRemove = "remove"
)
