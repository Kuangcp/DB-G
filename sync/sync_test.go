package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyKCVAndCipher(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidKey(key) {
		t.Fatal("new key must be 32 bytes")
	}
	kcv := KCV(key)
	if len(kcv) != 6 {
		t.Fatalf("kcv: %q", kcv)
	}
	if KCV(key) != kcv || !KCVEqual(kcv, strings.ToLower(kcv)) {
		t.Fatal("KCV must be stable and case-insensitive")
	}
	other, _ := NewKey()
	if KCV(other) == kcv {
		t.Fatal("different keys should have different KCV")
	}

	enc, err := Encrypt(key, []byte("host=db password=s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc) || !strings.HasPrefix(enc, EncPrefix) {
		t.Fatalf("ciphertext: %q", enc)
	}
	plain, err := Decrypt(key, enc)
	if err != nil || string(plain) != "host=db password=s3cret" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
	if _, err := Decrypt(other, enc); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestParseKeyString(t *testing.T) {
	key, _ := NewKey()
	for _, s := range []string{KeyString(key), strings.ToUpper(hexString(key))} {
		got, err := ParseKeyString(s)
		if err != nil || !bytes.Equal(got, key) {
			t.Fatalf("parse %q: %v", s, err)
		}
	}
	if _, err := ParseKeyString("short"); err == nil {
		t.Fatal("bad key must fail")
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		out = append(out, digits[v>>4], digits[v&0xf])
	}
	return string(out)
}

func TestKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sync.key")
	key, _ := NewKey()
	if err := SaveKeyFile(path, key); err != nil {
		t.Fatal(err)
	}
	got, err := LoadKeyFile(path)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("load: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v", info.Mode().Perm())
	}
}

func TestBuildAndReadTree(t *testing.T) {
	dir := t.TempDir()
	key, _ := NewKey()
	eng := NewEngine(filepath.Join(dir, "repo"), "origin", "main", "")
	snap := Snapshot{
		Conns:      []Conn{{ID: "c1", Name: "本地", Kind: "sqlite", Path: "/tmp/a.db", Password: "pw"}},
		Folders:    []Folder{{ID: "f1", Name: "dev"}},
		Workspaces: []Workspace{{ID: "w1", Name: "默认", Members: []string{"s1"}}},
		Consoles:   []Console{{Meta: ConsoleMeta{ID: "s1", ConnectionID: "c1", Name: "查询"}, Body: "select 1;"}},
	}
	files, err := eng.BuildFiles(key, snap, KCV(key))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[ConnectionPath("c1")]; !ok {
		t.Fatal("connection file missing")
	}
	plainConn, err := Decrypt(key, string(files[ConnectionPath("c1")]))
	if err != nil || !strings.Contains(string(plainConn), "\"password\": \"pw\"") {
		t.Fatalf("connection payload: %q %v", plainConn, err)
	}
	if _, ok := files[KCVFile]; !ok {
		t.Fatal("kcv missing")
	}
	if _, err := eng.WriteFiles(files); err != nil {
		t.Fatal(err)
	}
	// 相同快照复用同一份密文，避免随机 nonce 产生噪音。
	files2, _ := eng.BuildFiles(key, snap, KCV(key))
	if string(files2[ConnectionPath("c1")]) != string(files[ConnectionPath("c1")]) {
		t.Fatal("unchanged connection must reuse ciphertext")
	}
	snap.Conns[0].Name = "改名"
	files3, _ := eng.BuildFiles(key, snap, KCV(key))
	if string(files3[ConnectionPath("c1")]) == string(files[ConnectionPath("c1")]) {
		t.Fatal("changed connection must re-encrypt")
	}
	if _, err := eng.WriteFiles(files3); err != nil {
		t.Fatal(err)
	}
	tree, _, err := eng.ReadTree(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Conns) != 1 || tree.Conns[0].Name != "改名" || tree.Conns[0].Password != "pw" {
		t.Fatalf("tree conn: %+v", tree.Conns)
	}
	if len(tree.Consoles) != 1 || tree.Consoles[0].Body != "select 1;" {
		t.Fatalf("tree console: %+v", tree.Consoles)
	}
	other, _ := NewKey()
	if _, _, err := eng.ReadTree(other); err == nil {
		t.Fatal("wrong key must fail reading")
	}
}

func TestWriteFilesSweepsStaleEntities(t *testing.T) {
	dir := t.TempDir()
	key, _ := NewKey()
	eng := NewEngine(filepath.Join(dir, "repo"), "origin", "main", "")
	files, _ := eng.BuildFiles(key, Snapshot{
		Conns: []Conn{{ID: "c1", Name: "a", Kind: "sqlite"}},
	}, KCV(key))
	if _, err := eng.WriteFiles(files); err != nil {
		t.Fatal(err)
	}
	files, _ = eng.BuildFiles(key, Snapshot{}, KCV(key))
	stats, err := eng.WriteFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Deleted != 1 {
		t.Fatalf("stale connection must be deleted: %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(eng.WorkTree, filepath.FromSlash(ConnectionPath("c1")))); !os.IsNotExist(err) {
		t.Fatalf("stale file remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(eng.WorkTree, "README.md")); err != nil {
		t.Fatal("README should be kept")
	}
}
