package engine

import "testing"

func TestDriverMetaRegistry(t *testing.T) {
	f := func(Profile) (Session, error) { return stubSession{}, nil }
	Register("meta-z", DriverMeta{Order: 20, Label: "Zeta", DefaultPort: 1234, DefaultProtocol: "native", Protocols: []string{"native", "http"}, CountExpr: "count()", QuoteIdent: func(s string) string { return "`" + s + "`" }}, f)
	Register("meta-a", DriverMeta{Order: 10, Label: "Alpha", DefaultPort: 4321, QuoteIdent: func(s string) string { return `"` + s + `"` }}, f)

	m, ok := Meta("meta-z")
	if !ok || m.Label != "Zeta" || m.DefaultPort != 1234 || m.DefaultProtocol != "native" || m.CountExpr != "count()" {
		t.Fatalf("meta label=%q port=%d proto=%q count=%q ok=%v", m.Label, m.DefaultPort, m.DefaultProtocol, m.CountExpr, ok)
	}
	if m.QuoteIdent == nil || m.QuoteIdent("x") != "`x`" {
		t.Fatalf("quote func missing or wrong")
	}
	if _, ok := Meta("nope"); ok {
		t.Fatal("unknown meta must be false")
	}
	metas := DriverMetas()
	if len(metas) < 2 || metas[0].Order > metas[1].Order {
		t.Fatalf("metas not ordered: %+v", metas)
	}
}
