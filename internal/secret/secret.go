// Package secret 用 AES-256-GCM 加密保存在数据库中的敏感信息（模型 API Key）。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Box 负责加密与解密。
type Box struct {
	aead cipher.AEAD
}

// ErrDecrypt 表示无法解密（密钥不对或数据损坏）。
var ErrDecrypt = errors.New("无法解密已保存的 API Key（加密密钥可能已更换），请重新填写")

const prefix = "v1:"

// New 用 32 字节密钥创建 Box。
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("加密密钥必须是 32 字节，当前 %d 字节", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Load 按以下顺序取得密钥：配置值（64 位十六进制）→ <dataDir>/secret.key（不存在时生成）。
func Load(configured, dataDir string) (*Box, string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		key, err := hex.DecodeString(configured)
		if err != nil {
			return nil, "", fmt.Errorf("security.encryption_key 必须是 64 位十六进制: %w", err)
		}
		b, err := New(key)
		return b, "config", err
	}
	path := filepath.Join(dataDir, "secret.key")
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, derr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if derr != nil {
			return nil, "", fmt.Errorf("%s 内容无效: %w", path, derr)
		}
		b, err := New(key)
		return b, path, err
	case errors.Is(err, fs.ErrNotExist):
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, "", err
		}
		if err := os.MkdirAll(dataDir, 0o750); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
			return nil, "", fmt.Errorf("保存加密密钥: %w", err)
		}
		b, err := New(key)
		return b, path, err
	default:
		return nil, "", fmt.Errorf("读取加密密钥: %w", err)
	}
}

// Seal 加密明文，返回可存入数据库的字符串。空字符串原样返回。
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open 解密 Seal 的结果。
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	data, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return "", ErrDecrypt
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	n := b.aead.NonceSize()
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// Mask 返回用于展示的打码 Key，例如 sk-…a1b2。
func Mask(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "••••"
	}
	head := key[:3]
	if !strings.HasPrefix(key, "sk-") {
		head = key[:2]
	}
	return head + "…" + key[len(key)-4:]
}
