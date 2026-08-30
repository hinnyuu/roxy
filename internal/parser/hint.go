package parser

import (
	"regexp"
	"strings"

	"github.com/hinnyuu/roxy/internal/domain"
)

// hintRule 类型提示规则：bracket=括号精确匹配（优先），word=词边界匹配。
type hintRule struct {
	hint string
	re   *regexp.Regexp
	brk  bool
	name string
}

var hintRules = []hintRule{
	{hint: domain.HintOP, brk: true, re: regexp.MustCompile(`(?i)^(nc)?op$`)},
	{hint: domain.HintED, brk: true, re: regexp.MustCompile(`(?i)^(nc)?ed$`)},
	{hint: domain.HintPV, brk: true, re: regexp.MustCompile(`(?i)^pv\d*$`)},
	{hint: domain.HintCM, brk: true, re: regexp.MustCompile(`(?i)^cm\d*$`)},
	{hint: domain.HintOVA, brk: true, re: regexp.MustCompile(`(?i)^ova\d*$`)},
	{hint: domain.HintSpecial, brk: true, re: regexp.MustCompile(`(?i)^(sp|special|final)$`)},
	{hint: domain.HintMovie, brk: true, re: regexp.MustCompile(`(?i)^(movie|film|剧场版|劇場版|电影版|電影版)$`)},
	// Go RE2 的 \b 是 ASCII 词边界，对 CJK 无效：中文关键词不带边界。
	{hint: domain.HintOP, re: regexp.MustCompile(`(?i)\bnc-?op\b`)},
	{hint: domain.HintED, re: regexp.MustCompile(`(?i)\bnc-?ed\b`)},
	{hint: domain.HintPV, re: regexp.MustCompile(`(?i)\b(pv|trailer|preview)\b|预告|預告`)},
	{hint: domain.HintCM, re: regexp.MustCompile(`(?i)\bcm\b`)},
	{hint: domain.HintOVA, re: regexp.MustCompile(`(?i)\b(ova|oad)\b`)},
	{hint: domain.HintMovie, re: regexp.MustCompile(`(?i)\bmovie\b|剧场版|劇場版|电影版|電影版`)},
	{hint: domain.HintSpecial, re: regexp.MustCompile(`(?i)\b(special|final)\b|特别篇|特別篇|特典`)},
}

// findTypeHint 返回集类型提示与命中的提示名。groups 为文件名括号内容。
func findTypeHint(stripped string, groups []string) (string, string) {
	for _, hr := range hintRules {
		if !hr.brk {
			continue
		}
		for _, g := range groups {
			if hr.re.MatchString(strings.TrimSpace(toHalfWidth(g))) {
				return hr.hint, "hint-" + hr.hint
			}
		}
	}
	for _, hr := range hintRules {
		if hr.brk {
			continue
		}
		if hr.re.MatchString(toHalfWidth(stripped)) {
			return hr.hint, "hint-" + hr.hint
		}
	}
	return domain.HintUnknown, ""
}
