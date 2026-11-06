package engine

import (
	"fmt"
	"strings"
)

// Params 把「连接参数」拆成 key=value 对，给各驱动拼进自己的连接串。
//
// 分隔符可以是 & 、逗号、空白或换行，所以 "compress=0&secure=true" 和分行写的
// 多行文本都行；值里可以再出现 '='（按第一个切）。空串返回 nil。
func Params(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	out := map[string]string{}
	for _, tok := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '&' || r == ',' || r == '\n' || r == '\r' || r == ';' || r == ' ' || r == '\t'
	}) {
		k, v, ok := strings.Cut(tok, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("参数格式应为 key=value：%q", tok)
		}
		out[k] = strings.TrimSpace(v)
	}
	return out, nil
}
