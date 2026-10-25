package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Credentials 按服务器地址保存登录令牌（文件权限 0600）。
type Credentials struct {
	path   string
	Tokens map[string]string `json:"tokens"`
}

// LoadCredentials 读取令牌文件，不存在时返回空集合。
func LoadCredentials(path string) (*Credentials, error) {
	c := &Credentials{path: path, Tokens: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取登录信息: %w", err)
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("解析登录信息 %s: %w", path, err)
	}
	if c.Tokens == nil {
		c.Tokens = map[string]string{}
	}
	return c, nil
}

// Get 返回某服务器的令牌。
func (c *Credentials) Get(server string) string { return c.Tokens[server] }

// Set 保存或删除（token 为空）某服务器的令牌并写回文件。
func (c *Credentials) Set(server, token string) error {
	if token == "" {
		delete(c.Tokens, server)
	} else {
		c.Tokens[server] = token
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("创建目录: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("保存登录信息: %w", err)
	}
	return os.Rename(tmp, c.path)
}
