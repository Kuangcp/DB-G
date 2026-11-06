package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const secretKeyLen = 32 // AES-256

// SecretKeyPath 返回密钥文件路径（与连接库同目录）。
func SecretKeyPath(storePath string) string {
	return filepath.Join(filepath.Dir(storePath), "secret.key")
}

// Vault 负责密码的本机加解密；密文格式 enc:v1:<base64(nonce|cipher)>。
type Vault struct {
	key []byte
}

// LoadVault 读取密钥；缺失时生成新密钥（0600）。
func LoadVault(storePath string) (*Vault, error) {
	path := SecretKeyPath(storePath)
	data, err := os.ReadFile(path)
	if err == nil && len(data) == secretKeyLen {
		return &Vault{key: data}, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, secretKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return &Vault{key: key}, nil
}

// Encrypt 加密明文；空串原样返回。
func (v *Vault) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if v == nil || len(v.key) != secretKeyLen {
		return "", errors.New("密钥不可用")
	}
	block, err := aes.NewCipher(v.key)
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
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密密文；非 enc: 前缀视为明文原样返回。
func (v *Vault) Decrypt(s string) (string, error) {
	if !strings.HasPrefix(s, "enc:v1:") {
		return s, nil
	}
	if v == nil || len(v.key) != secretKeyLen {
		return "", errors.New("密钥不可用")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "enc:v1:"))
	if err != nil {
		return "", fmt.Errorf("密文损坏: %w", err)
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文损坏")
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", errors.New("无法解密（密钥已变更？）")
	}
	return string(plain), nil
}
