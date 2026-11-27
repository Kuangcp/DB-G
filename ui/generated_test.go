package ui

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 图标表与生成物必须一致：改了 tools/lucideicons/main.mjs 却忘记重跑生成器会失败。
func TestIconTableMatchesGenerated(t *testing.T) {
	script, err := os.ReadFile("../tools/lucideicons/main.mjs")
	if err != nil {
		t.Fatal(err)
	}
	gen, err := os.ReadFile("icons_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	scriptRE := regexp.MustCompile(`(?m)^\s*(icon[A-Za-z0-9]+)\s*:`)
	genRE := regexp.MustCompile(`(?m)^\s*(icon[A-Za-z0-9]+)\s*=`)
	names := func(re *regexp.Regexp, data []byte) []string {
		var out []string
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			out = append(out, m[1])
		}
		sort.Strings(out)
		return out
	}
	want, got := names(scriptRE, script), names(genRE, gen)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("icons_gen.go is stale\nwant: %v\ngot:  %v", want, got)
	}
}

// 生成文件的头部必须带 Code generated / DO NOT EDIT，避免被手改后无人发现。
func TestGeneratedHeaders(t *testing.T) {
	for _, path := range []string{"icons_gen.go", "lobe/tokens_gen.go", "../locales/catalog_gen.go"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		head := string(data)
		if !strings.Contains(head, "Code generated") || !strings.Contains(head, "DO NOT EDIT") {
			t.Fatalf("%s lacks generated header", path)
		}
	}
}
