package app

import (
	"strings"
	"testing"

	"db-g/sync"
)

func TestScanSecretsHighConfidence(t *testing.T) {
	key, _ := sync.NewKey()
	files := map[string][]byte{
		"db-g/consoles/a.sql": []byte(strings.Join([]string{
			"select 1;",
			"postgres://app:Hunter2!@db.example.com:5432/orders",
			"ALTER USER app IDENTIFIED BY 'S3cretPass!';",
			"-- AKIA1234567890ABCDEF",
			"-----BEGIN OPENSSH PRIVATE KEY-----",
			"mysql --password=Hunter2! -u app",
		}, "\n")),
	}
	got := ScanSecrets(key, files, nil)
	rules := map[string]bool{}
	for _, f := range got {
		rules[f.Rule] = true
		if strings.Contains(f.Preview, "S3cretPass!") || strings.Contains(f.Preview, "Hunter2!") {
			t.Fatalf("preview leaked secret: %q", f.Preview)
		}
	}
	for _, want := range []string{"url-credentials", "ddl-password", "known-key", "private-key", "cli-password"} {
		if !rules[want] {
			t.Fatalf("missing rule %q in %+v", want, got)
		}
	}
}

func TestScanSecretsAvoidsPlaceholders(t *testing.T) {
	key, _ := sync.NewKey()
	files := map[string][]byte{
		"db-g/consoles/a.sql": []byte(strings.Join([]string{
			"-- password = 'not a secret, just a note'",
			"CREATE TABLE users (password VARCHAR(255), token TEXT);",
			"set password = '${DB_PASSWORD}';",
			"select * from users where token = '***';",
			"password = 'changeme'",
			"password = 'example123'",
			"password = 'aaaaaaaa'",
			"INSERT INTO users(password) VALUES ('123');",
			"select 'password = ' as label;",
		}, "\n")),
	}
	if got := ScanSecrets(key, files, nil); len(got) != 0 {
		t.Fatalf("placeholders must not be reported: %+v", got)
	}
}

func TestScanSecretsMediumAndAccepted(t *testing.T) {
	key, _ := sync.NewKey()
	files := map[string][]byte{
		"db-g/consoles/a.sql": []byte("password = 'ActualPass123'\nINSERT INTO accounts(api_key) VALUES ('abcdef123456');"),
	}
	got := ScanSecrets(key, files, nil)
	if len(got) != 2 {
		t.Fatalf("findings: %+v", got)
	}
	accepted := map[string]bool{got[0].Fingerprint: true}
	again := ScanSecrets(key, files, accepted)
	if len(again) != 1 || again[0].Fingerprint != got[1].Fingerprint {
		t.Fatalf("accepted finding must be skipped: %+v", again)
	}
	// 换一把密钥后指纹变化，确认记录不会误套到别的密钥上。
	other, _ := sync.NewKey()
	if len(ScanSecrets(other, files, accepted)) != 2 {
		t.Fatal("fingerprints must be key-bound")
	}
}

func TestScanSecretsIgnoresNonSQL(t *testing.T) {
	key, _ := sync.NewKey()
	files := map[string][]byte{
		"db-g/consoles/a.meta.json": []byte(`{"name":"password = 'Hunter2!'"}`),
	}
	if got := ScanSecrets(key, files, nil); len(got) != 0 {
		t.Fatalf("only sql files: %+v", got)
	}
}
