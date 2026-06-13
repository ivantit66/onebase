package pdfimport

import "strings"

// toLowerASCII — быстрый ToLower только для ASCII (имена шрифтов латинские).
func toLowerASCII(s string) string { return strings.ToLower(s) }

// containsAny сообщает, содержит ли s любую из подстрок subs.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

const ptPerMM = 72.0 / 25.4 // 1 мм = 2.8346 pt

// ptToMM конвертирует пункты в миллиметры.
func ptToMM(pt float64) float64 { return pt * 25.4 / 72.0 }
