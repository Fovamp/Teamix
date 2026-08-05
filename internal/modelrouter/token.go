package modelrouter

import (
	"unicode"

	"reasonix/internal/provider"
)

// EstimateTokens 粗略估算一组消息的 token 数，用于窗口预检。
// 区分 CJK 与拉丁字符（盲区 D）：中文 1 字符 ≈ 0.6-1 token（取 0.75），
// 拉丁 ≈ 4 字符/token（0.25）。只统计消息 Content——tool 结果文本都在其中，
// 工具调用参数占比小，P0 忽略。
func EstimateTokens(msgs []provider.Message) int {
	var cjk, latin int
	for _, m := range msgs {
		c, l := countChars(m.Content)
		cjk += c
		latin += l
	}
	return cjk*3/4 + latin/4 + 4 // 少量常数抵消边界开销
}

// countChars 把 content 拆成 CJK 与非 CJK 字符数。
func countChars(s string) (cjk, latin int) {
	for _, r := range s {
		if isCJK(r) {
			cjk++
		} else {
			latin++
		}
	}
	return cjk, latin
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
