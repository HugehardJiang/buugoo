package proactive

import (
	"context"
	"encoding/json"
	"time"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 主动关心的「只观察」模式：照常判断要不要主动找对方，但不发出去，把「本来会发什么」记下来，
// 管理员在后台看着合适了再改成真的发。只留最近 maxObserved 条，存在 app_meta 里。

const (
	metaObserved = "proactive.observed"
	maxObserved  = 200
)

// Observation 是一条「本来会主动发的消息」。
type Observation struct {
	UserID   uint64    `json:"user_id"`
	Username string    `json:"username"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}

// Observed 返回记下的观察（最新的在前）。
func Observed(ctx context.Context, meta store.MetaRepo) []Observation {
	var out []Observation
	if raw, ok, err := meta.Get(ctx, metaObserved); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func (e *Engine) observe(ctx context.Context, u *store.User, reason string, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	list := Observed(ctx, e.store.Meta())
	list = append([]Observation{{UserID: u.ID, Username: u.Username, Reason: reason, At: now}}, list...)
	if len(list) > maxObserved {
		list = list[:maxObserved]
	}
	b, _ := json.Marshal(list)
	if err := e.store.Meta().Set(ctx, metaObserved, string(b)); err != nil {
		e.log.Warn("记录主动关心的观察失败", "err", err)
		return
	}
	e.log.Info("只观察：本来会主动发消息", "user_id", u.ID, "reason", reason)
}
