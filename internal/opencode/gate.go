package opencode

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

// 多用户版里 OpenCode Go 默认不开放（Go 套餐是给个人编程用的，拿来给很多人用不太合规）：
// 已经添加过 OpenCode 模型的站点照常可用；没添加过的，站长在管理后台的登录框里输入启用口令才打开。
// 口令只在代码里存 bcrypt 哈希，不存明文；输入口令只打开这个功能，不会登录管理后台。

// metaUnlocked 记下站长已经用口令打开了 OpenCode 接入。
const metaUnlocked = "opencode.unlocked"

// unlockHash 是启用口令的 bcrypt 哈希。
const unlockHash = "$2a$12$e/X9gXHYExNLYDnkNL3fEOgr4zFSw7.P9LYjN1nJpM3qVs77GxvTe"

// IsUnlockCode 报告输入的是不是启用口令。
func IsUnlockCode(s string) bool {
	if s == "" || len(s) > 72 {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(unlockHash), []byte(s)) == nil
}

// Enabled 报告多用户版里 OpenCode 接入是否打开：输入过启用口令，或者已经添加过 OpenCode 模型。
func (i *Importer) Enabled(ctx context.Context) bool {
	if v, ok, err := i.meta.Get(ctx, metaUnlocked); err == nil && ok && v == "1" {
		return true
	}
	return i.hasModels(ctx)
}

// Unlock 打开 OpenCode 接入（重启后也保持）。
func (i *Importer) Unlock(ctx context.Context) error {
	return i.meta.Set(ctx, metaUnlocked, "1")
}
