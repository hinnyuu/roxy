package parser

import (
	"regexp"
	"strings"
)

// DefaultLangMap 字幕语言标签映射默认表（docs/ARCHITECTURE.md §9.3，可经
// settings 覆盖）。键为归一化标签（小写半角），值为标准后缀。
func DefaultLangMap() map[string]string {
	return map[string]string{
		"jpsc": "zh-CN",
		"jptc": "zh-TW",
		"sc":   "zh-CN",
		"tc":   "zh-TW",
		"jp":   "ja",
		"chs":  "zh-CN",
		"cht":  "zh-TW",
		"gb":   "zh-CN",
		"big5": "zh-TW",
		"简":    "zh-CN",
		"繁":    "zh-TW",
	}
}

var suffixLangRe = regexp.MustCompile(`(?i)\.(zh-cn|zh-tw|zh-hk|zh|ja|en|ko|raw)$`)

var canonicalLang = map[string]string{
	"zh-cn": "zh-CN", "zh": "zh-CN", "zh-hk": "zh-TW", "zh-tw": "zh-TW",
	"ja": "ja", "en": "en", "ko": "ko", "raw": "raw",
}

// subtitleLangTag 从字幕 basename 尾部提取语言标签并返回去标签后的视频
// basename（D-013 降级链第 0 级输入）。两级探测：
//  1. 尾部 `.zh-CN` 式后缀；
//  2. 尾部 `[JPSC]` / `[CHS&CHT]` 式括号标签（映射表可编辑）。
func (p *Parser) subtitleLangTag(base string) (string, string) {
	if m := suffixLangRe.FindStringSubmatchIndex(base); m != nil {
		lang := canonicalLang[strings.ToLower(base[m[2]:m[3]])]
		if lang == "" {
			return "", base
		}
		return lang, base[:m[0]]
	}
	groups, stripped := extractBrackets(base)
	if len(groups) == 0 {
		return "", base
	}
	last := strings.TrimSpace(toHalfWidth(groups[len(groups)-1]))
	parts := strings.FieldsFunc(last, func(r rune) bool {
		return r == '&' || r == '+' || r == '/' || r == ' '
	})
	if len(parts) == 0 {
		return "", base
	}
	var langs []string
	seen := map[string]bool{}
	for _, part := range parts {
		l, ok := p.langMap[normalizeKey(part)]
		if !ok {
			return "", base
		}
		if !seen[l] {
			seen[l] = true
			langs = append(langs, l)
		}
	}
	// 去尾部语言括号后的残留即视频 basename（stripped 中该括号已替换为空格，
	// 需还原：以最后一个语言括号在原串中的位置截断）。
	idx := strings.LastIndex(base, "["+groups[len(groups)-1]+"]")
	if idx < 0 {
		idx = strings.LastIndex(base, groups[len(groups)-1])
	}
	if idx > 0 {
		return strings.Join(langs, ","), strings.TrimRight(base[:idx], " ")
	}
	return strings.Join(langs, ","), strings.TrimRight(stripped, " ")
}
