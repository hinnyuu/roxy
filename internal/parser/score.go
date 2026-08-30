package parser

import "github.com/hinnyuu/roxy/internal/domain"

// score 规则命中强度（D-014 置信度分解中的规则项）：
// 强模板（SxxExx / 括号集数）=1.0；带标题的显式集数=0.9；
// 剧场版等类型提示无集数=0.7；其他类型提示=0.6；合集包=0.5；裸数字=0.6；无信息=0.4。
func score(pr *domain.ParseResult, ep epMatch, hintName string) (float64, string) {
	switch {
	case ep.Batch:
		return 0.5, "batch"
	case ep.Rule == "s-e" || ep.Rule == "bracket-ep":
		return 1.0, ep.Rule
	case ep.Rule == "cn-ep" || ep.Rule == "ep-prefix" || ep.Rule == "dash-ep":
		if len(pr.TitleCandidates) > 0 {
			return 0.9, ep.Rule
		}
		return 0.6, ep.Rule
	case ep.Rule == "" && hintName != "":
		if pr.EPTypeHint == domain.HintMovie {
			return 0.7, hintName
		}
		return 0.6, hintName
	default:
		return 0.4, "none"
	}
}
