package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultRoundTripAndFilePerms(t *testing.T) {
	dir := t.TempDir()
	v, err := LoadVault(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := v.Encrypt("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "enc:v1:") || strings.Contains(enc, "s3cret") {
		t.Fatalf("ciphertext: %q", enc)
	}
	plain, err := v.Decrypt(enc)
	if err != nil || plain != "s3cret" {
		t.Fatalf("decrypt: %q err=%v", plain, err)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key perms: %v err=%v", info.Mode().Perm(), err)
	}
	// 明文原样返回
	if got, err := v.Decrypt("plain"); err != nil || got != "plain" {
		t.Fatalf("plain passthrough: %q %v", got, err)
	}
	// 换一把钥匙 → 解密失败
	v2, _ := LoadVault(filepath.Join(t.TempDir(), "connections.json"))
	if _, err := v2.Decrypt(enc); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestStoreEncryptsPasswordOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	s := LoadStore(path)
	if err := s.Add(ConnProfile{Name: "m", Kind: "mysql", Host: "h", Port: 3306, User: "u", Password: "topsecret"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "topsecret") {
		t.Fatalf("plaintext password on disk: %s", raw)
	}
	re := LoadStore(path)
	p := re.List()[0]
	if p.Password != "topsecret" {
		t.Fatalf("password round trip: %q", p.Password)
	}
}

func TestStoreMigratesLegacyPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	legacy := `{"conns":[{"id":"a","name":"old","kind":"mysql","host":"h","port":3306,"password":"legacy"}],"activeID":"a"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if got := s.List()[0].Password; got != "legacy" {
		t.Fatalf("loaded password: %q", got)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "legacy") || !strings.Contains(string(raw), "enc:v1:") {
		t.Fatalf("legacy password not encrypted in place: %s", raw)
	}
	if got := LoadStore(path).List()[0].Password; got != "legacy" {
		t.Fatalf("after migration: %q", got)
	}
}

func TestStoreCorruptCiphertextWarnsAndClears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	if _, err := LoadVault(path); err != nil { // 先创建密钥
		t.Fatal(err)
	}
	bad := `{"conns":[{"id":"a","name":"bad","kind":"mysql","host":"h","port":3306,"password":"enc:v1:AAAA"}],"activeID":""}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	s := LoadStore(path)
	if got := s.List()[0].Password; got != "" {
		t.Fatalf("corrupt password must be cleared: %q", got)
	}
	if !strings.Contains(s.Warning(), "无法解密") {
		t.Fatalf("warning: %q", s.Warning())
	}
}
