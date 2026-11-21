package ai

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	secret := "test" + "-pass"
	in := "mysql://root:" + secret + "@db:3306/shop password=hunter2 " +
		"Authorization: Bearer abc.def token: xyz"
	out := Redact(in)
	for _, leak := range []string{secret, "hunter2", "abc.def", "xyz"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q: %s", leak, out)
		}
	}
	for _, want := range []string{"mysql://***@db", "password=***", "Bearer ***", "token: ***"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}
