package locales

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
	"unicode"
)

// TestCatalogsAligned 保证 en 与 zh-Hans 的 key、占位符一致，且英文目录不含中文。
func TestCatalogsAligned(t *testing.T) {
	en := readCatalog(t, "en.json")
	zh := readCatalog(t, "zh-Hans.json")
	if len(en) == 0 {
		t.Fatal("en.json is empty")
	}
	placeholder := regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)[,}]`)
	for key, enText := range en {
		zhText, ok := zh[key]
		if !ok {
			t.Errorf("zh-Hans missing key %q", key)
			continue
		}
		want := placeholder.FindAllStringSubmatch(enText, -1)
		got := placeholder.FindAllStringSubmatch(zhText, -1)
		wantSet, gotSet := map[string]bool{}, map[string]bool{}
		for _, m := range want {
			wantSet[m[1]] = true
		}
		for _, m := range got {
			gotSet[m[1]] = true
		}
		for name := range wantSet {
			if !gotSet[name] {
				t.Errorf("%s: zh missing placeholder %q", key, name)
			}
		}
		for name := range gotSet {
			if !wantSet[name] {
				t.Errorf("%s: zh has extra placeholder %q", key, name)
			}
		}
		if hasHan(enText) && !autonyms[key] {
			t.Errorf("en %q contains Chinese: %q", key, enText)
		}
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Errorf("zh-Hans has extra key %q", key)
		}
	}
}

// autonyms 是菜单里按自身语言书写的语言名，允许在英文目录里含中文。
var autonyms = map[string]bool{"languageChinese": true}

func readCatalog(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	out := map[string]string{}
	for k, v := range raw {
		if len(k) > 0 && k[0] == '@' {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
