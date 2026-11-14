package sync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeySize 是同步密钥长度（AES-256）。
const KeySize = 32

// EncPrefix 是仓库内密文的版本前缀。
const EncPrefix = "dbg-sync:v1:"

// NewKey 生成一个随机同步密钥。
func NewKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// ValidKey 报告密钥长度是否可用。
func ValidKey(key []byte) bool { return len(key) == KeySize }

// LoadKeyFile 读取密钥文件；必须恰好 32 字节。
func LoadKeyFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != KeySize {
		return nil, fmt.Errorf("同步密钥长度不正确（%d 字节）", len(data))
	}
	return data, nil
}

// SaveKeyFile 原子写入密钥文件（0600，目录 0700），并尽量收紧已有文件权限。
func SaveKeyFile(path string, key []byte) error {
	if !ValidKey(key) {
		return errors.New("同步密钥长度不正确")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sync-key-*.tmp")
	if err != nil {
		return os.WriteFile(path, key, 0o600)
	}
	name := tmp.Name()
	if _, err := tmp.Write(key); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return os.WriteFile(path, key, 0o600)
	}
	return nil
}

// KeyString 返回可复制/粘贴的 base64 密钥。
func KeyString(key []byte) string { return base64.StdEncoding.EncodeToString(key) }

// ParseKeyString 解析 base64（标准/无填充/URL）或 64 位十六进制密钥。
func ParseKeyString(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("密钥为空")
	}
	if len(s) == KeySize*2 {
		if raw, err := hex.DecodeString(s); err == nil && len(raw) == KeySize {
			return raw, nil
		}
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if raw, err := enc.DecodeString(s); err == nil && len(raw) == KeySize {
			return raw, nil
		}
	}
	return nil, errors.New("密钥格式不正确（需要 32 字节的 base64 或 64 位十六进制）")
}

// KCV 返回标准 Key Check Value：AES-ECB(key, 16 个 0 字节) 的前 3 字节，6 位大写十六进制。
// 它只用于快速发现错钥，不参与数据加密；完整性仍由每个密文的 GCM tag 保证。
func KCV(key []byte) string {
	if !ValidKey(key) {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	var zero [aes.BlockSize]byte
	var out [aes.BlockSize]byte
	block.Encrypt(out[:], zero[:])
	return strings.ToUpper(hex.EncodeToString(out[:3]))
}

// KCVEqual 比较两个 KCV（constant time，避免把错误密钥的比对时间当信标）。
func KCVEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.ToUpper(strings.TrimSpace(a))),
		[]byte(strings.ToUpper(strings.TrimSpace(b)))) == 1
}

// IsEncrypted 报告文本是否带同步密文前缀。
func IsEncrypted(s string) bool { return strings.HasPrefix(s, EncPrefix) }

// Encrypt 用同步密钥加密明文，返回 dbg-sync:v1:<base64(nonce||ciphertext)>。
func Encrypt(key, plain []byte) (string, error) {
	if !ValidKey(key) {
		return "", errors.New("同步密钥不可用")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	return EncPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密同步密文；前缀缺失、损坏或密钥错误都返回错误。
func Decrypt(key []byte, stored string) ([]byte, error) {
	if !IsEncrypted(stored) {
		return nil, errors.New("不是同步密文")
	}
	if !ValidKey(key) {
		return nil, errors.New("同步密钥不可用")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, EncPrefix))
	if err != nil {
		return nil, fmt.Errorf("密文损坏: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("密文损坏")
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, errors.New("无法解密（同步密钥不匹配或仓库损坏）")
	}
	return plain, nil
}
