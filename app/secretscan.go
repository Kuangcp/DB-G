package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SecretFinding 是控制台 SQL 里疑似明文凭据的一处命中。
type SecretFinding struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Rule        string `json:"rule"`
	Severity    string `json:"severity"` // "high" | "medium"
	Preview     string `json:"preview"`
	Fingerprint string `json:"fingerprint"`
}

type secretRule struct {
	name     string
	severity string
	re       *regexp.Regexp
	// commentOnlyHigh：注释行只跑高置信规则；为 true 时中置信规则跳过注释。
	valueGroup int
}

var secretRules = []secretRule{
	{"private-key", "high", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`), 0},
	{"url-credentials", "high", regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s/@:]+:[^\s/@]+@`), 0},
	{"cli-password", "high", regexp.MustCompile(`(?i)(?:--password(?:=|\s+)|PGPASSWORD=|MYSQL_PWD=|--pass(?:word)?=)\s*['"]?([^\s'"]{4,})`), 1},
	{"ddl-password", "high", regexp.MustCompile(`(?i)(?:IDENTIFIED\s+(?:WITH\s+\S+\s+)?BY|SET\s+PASSWORD\s*=|PASSWORD\s+)\s*['"]([^'"]{4,})['"]`), 1},
	{"known-key", "high", regexp.MustCompile(`\b(?:AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9]{16,}|AIza[0-9A-Za-z_\-]{30,})\b`), 0},
	{"assignment", "medium", regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key)\b\s*[:=]\s*['"]([^'"]{4,})['"]`), 2},
	{"insert-secret", "medium", regexp.MustCompile(`(?i)\bINSERT\s+INTO\b[^;]*\b(password|passwd|pwd|secret|token|api[_-]?key)\b[^;]*\bVALUES\b[^;]*`), 0},
}

var quotedValue = regexp.MustCompile(`'([^']{4,})'|"([^"]{4,})"`)

// ScanSecrets 扫描即将写入仓库的 SQL 文件。accepted 是已确认的 fingerprint 集合。
// key 用于 fingerprint 的 HMAC，避免对低熵密码做离线字典反推。
func ScanSecrets(key []byte, files map[string][]byte, accepted map[string]bool) []SecretFinding {
	var out []SecretFinding
	paths := make([]string, 0, len(files))
	for path := range files {
		if strings.HasSuffix(path, ".sql") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		lines := strings.Split(strings.ReplaceAll(string(files[path]), "\r\n", "\n"), "\n")
		inBlock := false
		for i, line := range lines {
			comment := inBlock
			trimmed := strings.TrimSpace(line)
			if !comment && (strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#")) {
				comment = true
			}
			// 更新块注释状态（简单跟踪，足以避免把注释里的普通说明当成凭据）。
			for {
				start := strings.Index(line, "/*")
				if start < 0 {
					break
				}
				if end := strings.Index(line[start+2:], "*/"); end >= 0 {
					line = line[:start] + line[start+2+end+2:]
					continue
				}
				inBlock = true
				line = line[:start]
				break
			}
			if strings.Contains(line, "*/") {
				inBlock = false
			}
			for _, rule := range secretRules {
				if comment && rule.severity == "medium" {
					continue
				}
				loc := rule.re.FindStringSubmatchIndex(line)
				if loc == nil {
					continue
				}
				value := ""
				if rule.valueGroup > 0 && len(loc) > rule.valueGroup*2+1 {
					start, end := loc[rule.valueGroup*2], loc[rule.valueGroup*2+1]
					if start >= 0 {
						value = line[start:end]
					}
				}
				if value == "" {
					value = line[loc[0]:loc[1]]
				}
				if rule.name == "insert-secret" {
					if !insertHasRealValue(line) {
						continue
					}
				} else if rule.valueGroup > 0 && isPlaceholderSecret(value) {
					continue
				} else if rule.name == "url-credentials" || rule.name == "known-key" || rule.name == "private-key" {
					if isPlaceholderSecret(value) {
						continue
					}
				}
				fp := findingFingerprint(key, path, rule.name, line)
				if accepted[fp] {
					continue
				}
				out = append(out, SecretFinding{
					Path: path, Line: i + 1, Rule: rule.name,
					Severity: rule.severity, Preview: maskSecretLine(line),
					Fingerprint: fp,
				})
			}
		}
	}
	return out
}

func insertHasRealValue(line string) bool {
	for _, m := range quotedValue.FindAllStringSubmatch(line, -1) {
		for _, v := range m[1:] {
			if !isPlaceholderSecret(v) {
				return true
			}
		}
	}
	return false
}

func isPlaceholderSecret(v string) bool {
	t := strings.TrimSpace(strings.Trim(v, `'"`))
	if utf8.RuneCountInString(t) < 4 {
		return true
	}
	lower := strings.ToLower(t)
	for _, p := range []string{
		"{{", "${", "<", ">", "***", "****", "…", "...",
		"your_", "your-", "changeme", "change_me", "replace_me",
		"example", "placeholder", "redacted", "password", "passwd", "secret", "token",
	} {
		if strings.Contains(lower, p) {
			return true
		}
	}
	if strings.HasPrefix(lower, "$") || strings.HasPrefix(lower, "%") || strings.HasPrefix(lower, ":") {
		return true
	}
	first, _ := utf8.DecodeRuneInString(t)
	identical := true
	for _, r := range t {
		if r != first {
			identical = false
			break
		}
	}
	if identical {
		return true
	}
	// 只有标点/空白，没有可当凭据的内容。
	hasLetter := false
	for _, r := range t {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetter = true
			break
		}
	}
	return !hasLetter
}

// maskSecretLine 把引号里的长串与 URL 密码段替换成 ab***yz，避免把原值带进界面/日志。
func maskSecretLine(line string) string {
	masked := quotedValue.ReplaceAllStringFunc(line, func(m string) string {
		quote := m[:1]
		body := strings.Trim(m, `'"`)
		return quote + maskValue(body) + quote
	})
	urlRe := regexp.MustCompile(`://([^:/\s]+):([^@\s]+)@`)
	masked = urlRe.ReplaceAllString(masked, "://$1:***@")
	kvRe := regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|PGPASSWORD|MYSQL_PWD)(\s*[:=]\s*)([^\s'";]+)`)
	masked = kvRe.ReplaceAllStringFunc(masked, func(m string) string {
		loc := kvRe.FindStringSubmatchIndex(m)
		if len(loc) < 6 {
			return m
		}
		return m[loc[2]:loc[3]] + m[loc[4]:loc[5]] + maskValue(m[loc[6]:loc[7]])
	})
	if len(masked) > 160 {
		masked = masked[:160] + "…"
	}
	return strings.TrimSpace(masked)
}

func maskValue(v string) string {
	r := []rune(v)
	if len(r) <= 4 {
		return "***"
	}
	return string(r[:2]) + "***" + string(r[len(r)-2:])
}

func findingFingerprint(key []byte, path, rule, line string) string {
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%s\x00%s\x00%s", path, rule, strings.TrimSpace(line))
	return hex.EncodeToString(mac.Sum(nil))
}
