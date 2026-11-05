package ui

import "strings"

// dotIdent 返回光标前紧邻的 `ident.` 里的 ident（未加引号的 [A-Za-z_][A-Za-z0-9_$]*）。
//
// 只做词法近似：带引号的标识符（`"tbl".`）不解析；当前行在光标前有奇数个单引号
// （在字符串里）或 ident 之前出现 `--`（在行注释里）时不认。
func dotIdent(text string, offset int) (string, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	before := text[:offset]
	if len(before) == 0 || before[len(before)-1] != '.' {
		return "", false
	}
	body := before[:len(before)-1]
	i := len(body)
	for i > 0 && isIdentPart(body[i-1]) {
		i--
	}
	ident := body[i:]
	if ident == "" || !isIdentStart(ident[0]) {
		return "", false
	}
	lineStart := strings.LastIndexByte(body, '\n') + 1
	line := body[lineStart:]
	if strings.Count(line, "'")%2 == 1 {
		return "", false
	}
	if strings.Contains(line, "--") {
		return "", false
	}
	return ident, true
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9') || b == '$'
}
