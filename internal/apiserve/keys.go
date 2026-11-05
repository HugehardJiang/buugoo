// Package apiserve 是 API 接入：用户用自己的 API Key，通过 OpenAI（/v1/chat/completions）或
// Anthropic（/v1/messages）格式的接口调用自己能用的所有模型。只转发给模型，不经过布谷的人格、记忆和工具。
// 套餐里勾了「可以用 API」的用户（和管理员）才能用；非用户自己添加的模型按管理员设的倍率计费。
package apiserve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 限制。
const (
	MaxKeys      = 10
	MaxNameRunes = 30
	// KeyPrefix 是 API Key 的开头，方便认出来（也方便密钥扫描工具识别）。
	KeyPrefix = "sk-bg-"
	keyBody   = 40
)

// ErrNotFound 表示没有这个 Key。
var ErrNotFound = errors.New("没有这个 API Key")

// ErrInvalidKey 表示 Key 不对（不存在或已删除）。
var ErrInvalidKey = errors.New("API Key 无效")

// UserError 是填写的内容或操作不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Keys 管理 API Key。
type Keys struct {
	db  *gorm.DB
	now func() time.Time
}

// NewKeys 创建 Keys。
func NewKeys(db *gorm.DB) *Keys { return &Keys{db: db, now: time.Now} }

// KeyView 是列表里的一个 Key（不含原文）。
type KeyView struct {
	ID         uint64     `json:"id"`
	Name       string     `json:"name"`
	Hint       string     `json:"hint"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toView(k *store.APIKey) KeyView {
	return KeyView{ID: k.ID, Name: k.Name, Hint: k.Hint, LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt}
}

// HashKey 是数据库里存的 Key 摘要。Key 有 40 位随机字符（约 238 位熵），不需要加盐和慢哈希。
func HashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

func randomBody() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < keyBody; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String(), nil
}

// Create 新建一个 Key，返回原文（只这一次能看到）。
func (k *Keys) Create(ctx context.Context, userID uint64, name string) (string, KeyView, error) {
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return "", KeyView{}, &UserError{fmt.Sprintf("名字最多 %d 个字", MaxNameRunes)}
	}
	var n int64
	if err := k.db.WithContext(ctx).Model(&store.APIKey{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		return "", KeyView{}, err
	}
	if n >= MaxKeys {
		return "", KeyView{}, &UserError{fmt.Sprintf("最多 %d 个 API Key，先删掉不用的", MaxKeys)}
	}
	body, err := randomBody()
	if err != nil {
		return "", KeyView{}, err
	}
	raw := KeyPrefix + body
	row := &store.APIKey{UserID: userID, Name: name, Hash: HashKey(raw), Hint: KeyPrefix + body[:4] + "…" + body[len(body)-4:]}
	if err := k.db.WithContext(ctx).Create(row).Error; err != nil {
		return "", KeyView{}, err
	}
	return raw, toView(row), nil
}

// List 列出用户的 Key。
func (k *Keys) List(ctx context.Context, userID uint64) ([]KeyView, error) {
	var rows []store.APIKey
	if err := k.db.WithContext(ctx).Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]KeyView, len(rows))
	for i := range rows {
		out[i] = toView(&rows[i])
	}
	return out, nil
}

// Delete 删除用户的一个 Key，之后用它的请求立即失效。
func (k *Keys) Delete(ctx context.Context, userID, id uint64) error {
	res := k.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&store.APIKey{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Lookup 用原文找到 Key，并记下最近使用时间（最多每分钟写一次）。
func (k *Keys) Lookup(ctx context.Context, raw string) (*store.APIKey, error) {
	if !strings.HasPrefix(raw, KeyPrefix) || len(raw) != len(KeyPrefix)+keyBody {
		return nil, ErrInvalidKey
	}
	var row store.APIKey
	if err := k.db.WithContext(ctx).Where("hash = ?", HashKey(raw)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidKey
		}
		return nil, err
	}
	now := k.now()
	if row.LastUsedAt == nil || now.Sub(*row.LastUsedAt) > time.Minute {
		_ = k.db.WithContext(ctx).Model(&store.APIKey{}).Where("id = ?", row.ID).Update("last_used_at", now).Error
		row.LastUsedAt = &now
	}
	return &row, nil
}
