package engine

import "testing"

func TestParamsParsing(t *testing.T) {
	got, err := Params("compress=0&secure=true\ntls=true, sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"compress": "0", "secure": "true", "tls": "true", "sslmode": "require"}
	if len(got) != len(want) {
		t.Fatalf("params: %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func TestParamsEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\n"} {
		got, err := Params(raw)
		if err != nil || len(got) != 0 {
			t.Fatalf("%q: %v %v", raw, got, err)
		}
	}
}

func TestParamsValueWithEquals(t *testing.T) {
	got, err := Params("password=a=b")
	if err != nil {
		t.Fatal(err)
	}
	if got["password"] != "a=b" {
		t.Fatalf("value must keep its second '=': %v", got)
	}
}

func TestParamsRejectsBareToken(t *testing.T) {
	if _, err := Params("compress=0 bogus"); err == nil {
		t.Fatal("a token without '=' must be an error")
	}
}
