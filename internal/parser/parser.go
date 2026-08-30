// Package parser 规则引擎：解析动漫文件名（发布组命名模板、中括号、集数模式）。
// 设计见 docs/ARCHITECTURE.md §6 步骤 1-2 与 §9.1；LLM 兜底（M3）共用
// domain.ParseResult 形状。规则命中强度写入 ParseResult.Confidence，
// 供 matcher 置信度分解（D-014）。
package parser

import (
	"path/filepath"
	"strings"

	"github.com/hinnyuu/roxy/internal/domain"
)

// 文件类别（与 source_files.kind CHECK 对齐）。
const (
	KindVideo    = "video"
	KindSubtitle = "subtitle"
	KindNFO      = "nfo"
	KindImage    = "image"
	KindOther    = "other"
)

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".ts": true, ".m2ts": true,
	".wmv": true, ".flv": true, ".mov": true, ".webm": true, ".rmvb": true,
	".mpg": true, ".mpeg": true, ".m4v": true, ".mpv": true,
}

var subtitleExts = map[string]bool{
	".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true, ".idx": true,
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".bmp": true, ".gif": true,
}

// Classify 按扩展名判定文件类别。
func Classify(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case videoExts[ext]:
		return KindVideo
	case subtitleExts[ext]:
		return KindSubtitle
	case ext == ".nfo":
		return KindNFO
	case imageExts[ext]:
		return KindImage
	default:
		return KindOther
	}
}

// Parser 规则引擎入口。零配置可用；langMap 为字幕语言标签映射（§9.3，
// nil 用默认表），M5 起可注入用户自定义模式。
type Parser struct {
	langMap map[string]string
}

func New(langMap map[string]string) *Parser {
	if langMap == nil {
		langMap = DefaultLangMap()
	}
	return &Parser{langMap: langMap}
}

// Parse 解析单个文件名（含扩展名，不含目录）。
// 非视频/字幕文件返回 nil。
func (p *Parser) Parse(name string) *domain.ParseResult {
	kind := Classify(name)
	if kind != KindVideo && kind != KindSubtitle {
		return nil
	}
	base := toHalfWidth(strings.TrimSuffix(name, filepath.Ext(name)))

	pr := &domain.ParseResult{EPTypeHint: domain.HintUnknown}

	// 字幕先行：剥离尾部语言标签（D-013 第 0 级输入），再解析视频名。
	if kind == KindSubtitle {
		if lang, b2 := p.subtitleLangTag(base); lang != "" {
			pr.SubtitleLang = lang
			pr.SubtitleBase = b2
			base = b2
		}
	}

	// 1) 括号组提取（半/全角），首个括号视为发布组原始串（D-012）。
	brackets, stripped := extractBrackets(base)
	if len(brackets) > 0 {
		pr.ReleaseGroup = brackets[0]
		groups := brackets[1:]
		if isVersionTag(brackets[0]) {
			// 首括号不是发布组而是集数/技术标签时不占用 release_group
			pr.ReleaseGroup = ""
			groups = brackets
		}
		var labels []string
		for _, g := range groups {
			if isEpisodeBracket(g) || yearOnlyRe.MatchString(toHalfWidth(g)) {
				continue
			}
			labels = append(labels, g)
		}
		pr.VersionLabels = labels
	}

	// 2) 集数模式（§6 集数模式清单）。
	ep := findEpisode(stripped, brackets)
	pr.EpisodeRaw = ep.Raw
	pr.Episode = ep.Episode
	pr.EpisodeEnd = ep.EpisodeEnd
	pr.Season = ep.Season
	if ep.Batch {
		pr.Batch = true
	}

	// 3) 类型提示（NCOP/ED/PV/CM/OVA/剧场版/SP/FINAL…）。
	hint, hintName := findTypeHint(stripped, brackets)
	pr.EPTypeHint = hint

	// 4) 标题候选：去括号、去集数片段、去技术标签后的残留文本。
	pr.TitleCandidates = titleCandidates(stripped, ep.MatchSpan)
	pr.Year = findYear(brackets)

	// 5) 集标题：集数模式之后的残留（仅当有明确分隔符时采信）。
	if ep.MatchSpan[1] >= 0 && ep.MatchSpan[1] < len(stripped) {
		if t := cleanTitle(stripped[ep.MatchSpan[1]:]); t != "" && !isTechTag(t) {
			pr.EpisodeTitle = t
		}
	}

	// 6) version_key = 归一化发布组 + vN 修订号（D-012/D-011）。
	vkParts := []string{}
	if pr.ReleaseGroup != "" {
		vkParts = append(vkParts, normalizeKey(pr.ReleaseGroup))
	}
	if ep.Version != "" {
		vkParts = append(vkParts, strings.ToLower(ep.Version))
	}
	pr.VersionKey = strings.Join(vkParts, " ")

	// 7) 规则命中强度。
	pr.Confidence, pr.Rule = score(pr, ep, hintName)
	return pr
}
