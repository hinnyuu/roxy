package parser

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/hinnyuu/roxy/internal/domain"
)

// 归一化原语下沉至 domain（parser/metadata/matcher 共用）。
func normalizeKey(s string) string { return domain.NormalizeKey(s) }
func toHalfWidth(s string) string  { return domain.ToHalfWidth(s) }

// bracketRe 半角与全角括号并列：入口不做整体半角化，以保留标题原始字符
// （显示层忠实于文件名，D-012 归一化只用于比对键）。
var bracketRe = regexp.MustCompile(`\[([^\[\]]*)\]|【([^【】]*)】|\(([^()]*)\)|（([^（）]*)）|\{([^{}]*)\}`)

// extractBrackets 按出现顺序提取括号内容，并把括号连同其内容从字符串中移除。
func extractBrackets(s string) (groups []string, stripped string) {
	stripped = bracketRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := bracketRe.FindStringSubmatch(m)
		content := ""
		for _, g := range sub[1:] {
			if g != "" {
				content = g
				break
			}
		}
		groups = append(groups, strings.TrimSpace(content))
		return " "
	})
	return groups, stripped
}

// isEpisodeBracket 括号内容是否为集数形态（纯数字/vN/小数/区间）。
func isEpisodeBracket(g string) bool {
	return bracketEpRe.MatchString(strings.TrimSpace(g))
}

// isVersionTag 首括号是否其实是集数/技术标签而非发布组（此时不占用 release_group）。
func isVersionTag(g string) bool {
	g = strings.TrimSpace(g)
	return isEpisodeBracket(g) || isTechTag(g)
}

var techTags = []string{
	"720p", "1080p", "2160p", "480p", "576p", "360p", "4k", "8k", "hdr", "sdr", "dv",
	"x264", "x265", "h.264", "h264", "h.265", "h265", "hevc", "avc", "aac", "flac",
	"opus", "mp3", "ac3", "eac3", "dts", "dts-hd", "truehd", "10bit", "8bit", "hi10p",
	"hi444p", "web-dl", "webdl", "web", "blu-ray", "bluray", "bdrip", "brrip", "dvdrip",
	"dv", "bd", "remux", "multi", "dual audio", "dualaudio",
}

var techSet = func() map[string]bool {
	m := make(map[string]bool, len(techTags))
	for _, t := range techTags {
		m[t] = true
	}
	return m
}()

// isTechTag 判断括号/文本是否为技术标签。
func isTechTag(s string) bool {
	s = normalizeKey(s)
	if techSet[s] {
		return true
	}
	// 复合标签：x265_aac、AVC AAC、BIG5、GBK、GB、繁体、简体 等
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '_' || r == '&' || r == '+' || r == '/' || r == '.'
	}) {
		if techSet[part] {
			return true
		}
	}
	switch s {
	case "big5", "gb", "gbk", "chs", "cht", "chs&cht", "简", "繁", "简体", "繁体",
		"简体中文", "繁体中文", "中字", "中日双语", "日", "中":
		return true
	}
	return false
}

var yearRe = regexp.MustCompile(`^\((\d{4})\)$`)
var yearOnlyRe = regexp.MustCompile(`^\d{4}$`)

// findYear 从括号组中提取年份（剧场版常见 `(2020)`）。
func findYear(brackets []string) *int {
	for _, b := range brackets {
		if m := yearRe.FindStringSubmatch("(" + toHalfWidth(b) + ")"); m != nil {
			if y, err := strconv.Atoi(m[1]); err == nil {
				return &y
			}
		}
	}
	return nil
}

var titleCleanupRe = regexp.MustCompile(`\s+`)

// rawClean 保留原始字符的标题修剪：仅折叠空白并去首尾分隔符。
// 全角 ～/〜 不修剪：动漫标题惯用「～标题～」成对装饰（如官方名
// "无职转生～到了异世界就拿出真本事～"），它们几乎从不充当分隔符；
// 匹配侧 NormalizeTitle 会统一去除，不受影响。
func rawClean(s string) string {
	s = titleCleanupRe.ReplaceAllString(s, " ")
	return strings.Trim(s, " \t-~|_.—–")
}

// cleanTitle 修剪标题残留：折叠空白、去首尾分隔符。
func cleanTitle(s string) string {
	s = toHalfWidth(s)
	s = titleCleanupRe.ReplaceAllString(s, " ")
	s = strings.Trim(s, " -~|_.,:;!?&'\"")
	return strings.TrimSpace(s)
}

// titleCandidates 从去括号文本中提取标题候选：集数匹配之前的部分为主候选，
// 附带归一化变体（供 matcher 别名/FTS 检索逐级尝试）。
func titleCandidates(stripped string, epSpan [2]int) []string {
	head := stripped
	if epSpan[0] >= 0 {
		head = stripped[:epSpan[0]]
	}
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
	}
	add(rawClean(head))
	add(cleanTitle(head))
	add(cleanTitle(strings.TrimRightFunc(toHalfWidth(head), func(r rune) bool {
		return r == '!' || r == '？' || r == '?'
	})))
	// scene 点分隔命名惯例：Fate.Zero → Fate Zero
	add(cleanTitle(strings.ReplaceAll(head, ".", " ")))
	return out
}
