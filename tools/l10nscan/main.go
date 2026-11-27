// l10nscan 扫描 Go 源码里的中文字符串字面量，按国际化白名单报告残留。
//
// 用法：
//
//	go run ./tools/l10nscan        # 只报违规项，有则退出码 1
//	go run ./tools/l10nscan -all   # 列出全部中文字面量
//
// 白名单按“包目录前缀 + 文件”配置：驱动/同步/引擎的技术错误、AI 提示词、CLI 帮助与
// 生成的/示例数据不要求翻译；ui/ 与 app/ 的用户可见文案必须走 locales。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type finding struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// allowedPkg 是整个包允许保留中文文案的目录（相对仓库根，斜杠分隔）。
var allowedPkg = []string{
	"ai",      // 提示词与上下文模板，由 ContextSnapshot.Language 控制回答语言
	"ai/plan", // 计划解析的技术错误
	"engine",  // 协议无关层，不含文案翻译
	"driver",  // 驱动错误原样展示
	"sync",    // 系统 git 封装的技术错误
	"locales", // 生成的 catalog
	"tools",   // 生成器
	"ui/lobe", // 生成的配色 token
}

// allowedFile 是单个文件的白名单（保存的解释/示例数据，不是界面文案）。
var allowedFile = map[string]string{
	"ui/preset.go": "演示 SQL 数据",
}

// allowedText 是文件里允许保留的特定字面量（兼容旧持久化数据等）。
var allowedText = map[string]map[string]bool{
	"app/aichat.go": {"新会话": true}, // 旧版本持久化的默认标题，仅用于加载迁移
}

func main() {
	all := flag.Bool("all", false, "list every Chinese literal, including allowed ones")
	jsonOut := flag.Bool("json", false, "print violations as JSON")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fset := token.NewFileSet()
	var allFindings, violations []finding
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "dist" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isGenerated(data) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, data, 0)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || !hasHan(v) {
				return true
			}
			fd := finding{File: rel, Line: fset.Position(lit.Pos()).Line, Text: v}
			allFindings = append(allFindings, fd)
			if !allowed(rel, v) {
				violations = append(violations, fd)
			}
			return true
		})
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if *all {
		printFindings(allFindings)
	}
	if *jsonOut {
		printJSON(violations)
	} else if len(violations) > 0 {
		fmt.Printf("未国际化的中文文案 %d 条：\n", len(violations))
		printFindings(violations)
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
	fmt.Printf("l10nscan: ok（ui/app 无未翻译文案；白名单文件 %d 个）\n", len(allowedFile))
}

func allowed(rel, text string) bool {
	if reason, ok := allowedFile[rel]; ok && reason != "" {
		return true
	}
	if set, ok := allowedText[rel]; ok && set[text] {
		return true
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	for _, p := range allowedPkg {
		if dir == p || strings.HasPrefix(dir, p+"/") {
			return true
		}
	}
	if dir == "." {
		return true // main 包的 CLI 帮助/日志
	}
	return false
}

func printFindings(list []finding) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].File != list[j].File {
			return list[i].File < list[j].File
		}
		return list[i].Line < list[j].Line
	})
	for _, f := range list {
		text := strings.ReplaceAll(f.Text, "\n", "\\n")
		if r := []rune(text); len(r) > 120 {
			text = string(r[:120]) + "…"
		}
		fmt.Printf("  %s:%d  %s\n", f.File, f.Line, text)
	}
}

func printJSON(list []finding) {
	if len(list) == 0 {
		fmt.Println("[]")
		return
	}
	fmt.Print("[")
	for i, f := range list {
		if i > 0 {
			fmt.Print(",")
		}
		fmt.Printf(`{"file":%q,"line":%d,"text":%q}`, f.File, f.Line, f.Text)
	}
	fmt.Println("]")
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func isGenerated(data []byte) bool {
	head := string(data)
	if i := strings.IndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}
	return strings.Contains(head, "Code generated") && strings.Contains(head, "DO NOT EDIT")
}
