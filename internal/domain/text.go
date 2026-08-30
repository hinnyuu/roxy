package domain

import "strings"

var halfWidthReplacer = strings.NewReplacer(
	"【", "[", "】", "]", "（", "(", "）", ")", "〔", "[", "〕", "]",
	"「", "[", "」", "]",
	"！", "!", "？", "?", "：", ":", "；", ";", "，", ",", "。", ".",
	"＆", "&", "－", "-", "～", "~", "–", "-", "—", "-", "・", ".", "·", ".",
	"　", " ",
)

const fullWidthDigits = "０１２３４５６７８９"
const halfWidthDigits = "0123456789"

// ToHalfWidth 全角标点/数字/字母转半角（§6 归一化标题的第一步）。
func ToHalfWidth(s string) string {
	s = halfWidthReplacer.Replace(s)
	return strings.Map(func(r rune) rune {
		if i := strings.IndexRune(fullWidthDigits, r); i >= 0 {
			return rune(halfWidthDigits[i])
		}
		if r >= 'Ａ' && r <= 'Ｚ' {
			return r - 'Ａ' + 'A'
		}
		if r >= 'ａ' && r <= 'ｚ' {
			return r - 'ａ' + 'a'
		}
		return r
	}, s)
}

// NormalizeKey 归一化比对键：半角、小写、折叠空白（D-012 version_key 用）。
func NormalizeKey(s string) string {
	return strings.Join(strings.Fields(ToHalfWidth(strings.ToLower(s))), " ")
}

// NormalizeTitle 标题比对归一化：去标点与空白（§6 步骤 1a 本地别名匹配用）。
func NormalizeTitle(s string) string {
	s = ToHalfWidth(strings.ToLower(s))
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_', '.', ',', ';', ':', '!', '?', '\'', '"', '(', ')', '[', ']', '{', '}', '&', '~', '/':
			return -1
		}
		return r
	}, s)
}
