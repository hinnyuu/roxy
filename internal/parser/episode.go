package parser

import (
	"regexp"
	"strconv"
	"strings"
)

type epMatch struct {
	Raw        string
	Rule       string // bracket-ep | s-e | cn-ep | ep-prefix | dash-ep | ""
	Episode    *float64
	EpisodeEnd *float64
	Season     *int
	Version    string // vN 修订号
	Batch      bool   // 跨多集区间（>2 集），禁止自动定位
	MatchSpan  [2]int // stripped 中的 [start,end)；{-1,-1}=未匹配
}

// bracketEpRe 括号内集数形态：01 / 01v2 / 12.5 / 01-02 / EP01。
var bracketEpRe = regexp.MustCompile(`(?i)^(?:ep\.?\s*)?(\d{1,3}(?:\.\d)?)(v\d+)?(?:\s*-\s*(\d{1,3}(?:\.\d)?))?$`)

var seRe = regexp.MustCompile(`(?i)\bS(\d{1,2})E(?:P?)?(\d{1,3})(?:E(?:P?)?(\d{1,3}))?\b`)
var cnEpRe = regexp.MustCompile(`第\s*(\d{1,3}(?:\.\d)?)\s*[话話集]`)
var epPrefixRe = regexp.MustCompile(`(?i)\b(?:EP|Episode)\.?\s*(\d{1,3}(?:\.\d)?)(v\d+)?\b`)
var dashEpRe = regexp.MustCompile(`[\s._\-](\d{1,3}(?:\.\d)?)(v\d+)?[\s._\-]`)

// findEpisode 按优先级探测集数模式（ROADMAP M2 清单：[01]/- 01/第01话/EP01/01v2/12.5）。
// groups 为已提取的括号内容（括号在 stripped 中已被移除，需单独检查）。
func findEpisode(stripped string, groups []string) epMatch {
	m := epMatch{MatchSpan: [2]int{-1, -1}}

	for _, g := range groups {
		if sm := bracketEpRe.FindStringSubmatch(strings.TrimSpace(toHalfWidth(g))); sm != nil {
			ep := parseFloat(sm[1])
			m.Episode = &ep
			m.Raw = strings.TrimSpace(g)
			m.Version = sm[2]
			m.Rule = "bracket-ep"
			if sm[3] != "" {
				end := parseFloat(sm[3])
				m.EpisodeEnd = &end
				if end-*m.Episode > 1.0001 {
					m.Batch = true
				}
			}
			return m
		}
	}

	if sm := seRe.FindStringSubmatchIndex(stripped); sm != nil {
		season := atoi(stripped[sm[2]:sm[3]])
		ep := parseFloat(stripped[sm[4]:sm[5]])
		m.Season, m.Episode = &season, &ep
		m.Raw = stripped[sm[0]:sm[1]]
		m.Rule = "s-e"
		if sm[6] >= 0 {
			end := parseFloat(stripped[sm[6]:sm[7]])
			m.EpisodeEnd = &end
			if end-ep > 1.0001 {
				m.Batch = true
			}
		}
		m.MatchSpan = [2]int{sm[0], sm[1]}
		return m
	}

	for _, pe := range []struct {
		re   *regexp.Regexp
		name string
	}{
		{cnEpRe, "cn-ep"},
		{epPrefixRe, "ep-prefix"},
	} {
		if sm := pe.re.FindStringSubmatchIndex(stripped); sm != nil {
			ep := parseFloat(stripped[sm[2]:sm[3]])
			m.Episode = &ep
			m.Raw = stripped[sm[0]:sm[1]]
			m.Rule = pe.name
			if len(sm) > 4 && sm[4] >= 0 {
				m.Version = stripped[sm[4]:sm[5]]
			}
			m.MatchSpan = [2]int{sm[0], sm[1]}
			return m
		}
	}

	// 取最后一个匹配：集号惯例在标题之后（"Steins;Gate 0 - 01" 取 01 而非 0）。
	all := dashEpRe.FindAllStringSubmatchIndex(stripped+" ", -1)
	if len(all) > 0 {
		sm := all[len(all)-1]
		ep := parseFloat(stripped[sm[2]:sm[3]])
		m.Episode = &ep
		m.Raw = stripped[sm[2]:sm[3]]
		m.Rule = "dash-ep"
		if sm[4] >= 0 {
			m.Version = stripped[sm[4]:sm[5]]
		}
		m.MatchSpan = [2]int{sm[2], sm[3]}
		return m
	}

	return m
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
