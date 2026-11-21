package ai

import "regexp"

// 常见凭据形态的保守脱敏：只用于可选发送的历史/错误文本，不改当前 SQL。
var (
	reURLUserInfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
	reAuthScheme  = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+`)
	reSecretKV    = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)
)

// Redact 去掉 URL 凭据、Authorization 值和 password/token 之类的键值。
// 它是保守的提示词卫生处理，不追求覆盖所有数据库方言。
func Redact(s string) string {
	if s == "" {
		return ""
	}
	s = reURLUserInfo.ReplaceAllString(s, "$1***@")
	s = reAuthScheme.ReplaceAllString(s, "$1 ***")
	s = reSecretKV.ReplaceAllString(s, "$1$2***")
	return s
}
