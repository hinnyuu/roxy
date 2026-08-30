package domain

// ParseResult 是 source_files.parse_result 的 JSON 载荷：规则引擎（M2）与
// LLM parse schema（M3，docs/ARCHITECTURE.md §7.2）共用同一形状，
// 使 matcher 不区分解析来源。
type ParseResult struct {
	TitleCandidates []string `json:"title_candidates"`
	Season          *int     `json:"season_hint,omitempty"`    // S01E02 式季度号
	Year            *int     `json:"year,omitempty"`           // 括号年份（剧场版常见）
	EpisodeRaw      string   `json:"ep_number_raw"`            // 原始集数字符串（如 "01v2"、"12.5"、"01-02"）
	Episode         *float64 `json:"episode,omitempty"`        // 归一化起始集号
	EpisodeEnd      *float64 `json:"episode_end,omitempty"`    // 多集合一文件的结束集号
	EpisodeTitle    string   `json:"episode_title,omitempty"`  // 集数模式后的残留集标题
	EPTypeHint      string   `json:"ep_type_hint"`             // tv|special|ova|movie|op|ed|pv|cm|unknown
	ReleaseGroup    string   `json:"release_group"`            // 首个中括号原始字符串
	VersionKey      string   `json:"version_key"`              // 归一化后的中括号（配对键，D-012）
	VersionLabels   []string `json:"version_labels,omitempty"` // 其余括号内容（显示层候选）
	SubtitleLang    string   `json:"subtitle_lang,omitempty"`  // 字幕语言标签映射结果（§9.3）
	SubtitleBase    string   `json:"subtitle_base,omitempty"`  // 字幕去掉语言标签后的视频 basename
	Batch           bool     `json:"batch,omitempty"`          // 合集包（如 [01-12] 全集），不可自动定位单集
	Confidence      float64  `json:"confidence"`               // 规则命中强度 0–1
	Rule            string   `json:"rule"`                     // 命中规则名（可观测性）
}

// EPTypeHint 取值（ep_type_hint 与 bgm ep_type 的桥接，§6 步骤 2）。
const (
	HintTV      = "tv"
	HintSpecial = "special"
	HintOVA     = "ova"
	HintMovie   = "movie"
	HintOP      = "op"
	HintED      = "ed"
	HintPV      = "pv"
	HintCM      = "cm"
	HintUnknown = "unknown"
)
