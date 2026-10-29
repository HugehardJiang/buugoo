package models

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 辅助模型（整理记忆、决定要不要主动找对方、没有对话模型时的记忆提取和摘要）：
// 管理员可以在网页上直接选一个现有的模型（内置模型或公共模型），不选就用配置文件里的 llm.utility。

const metaUtility = "models.utility"

// utilityCacheTTL 是读取设置的缓存时间（辅助模型每次调用都要查一次选的是哪个）。
const utilityCacheTTL = 30 * time.Second

type utilitySetting struct {
	// ModelID 为空表示用配置文件里的辅助模型。
	ModelID *uint64 `json:"model_id"`
}

// UtilityView 是现在的辅助模型。
type UtilityView struct {
	// ModelID 是选的模型（0 是配置文件里的聊天模型），为空表示用配置文件里的辅助模型。
	ModelID *uint64 `json:"model_id"`
	// Name、Model 是实际使用的模型的名字和模型名。
	Name  string `json:"name"`
	Model string `json:"model"`
	// Config 是配置文件里的辅助模型名（不选时用它）。
	Config string `json:"config"`
	// Missing 表示选的模型已经删除或不能用了，暂时改用配置文件里的。
	Missing bool `json:"missing,omitempty"`
}

type utilityCache struct {
	mu  sync.Mutex
	at  time.Time
	set utilitySetting
}

func (s *Service) utilitySetting(ctx context.Context) utilitySetting {
	s.util.mu.Lock()
	defer s.util.mu.Unlock()
	if !s.util.at.IsZero() && time.Since(s.util.at) < utilityCacheTTL {
		return s.util.set
	}
	var st utilitySetting
	if raw, ok, err := s.store.Meta().Get(ctx, metaUtility); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	s.util.set, s.util.at = st, time.Now()
	return st
}

// utilityModel 返回选的辅助模型；没选或选的不能用了返回 nil（用配置文件里的）。
func (s *Service) utilityModel(ctx context.Context) (*Resolved, bool) {
	st := s.utilitySetting(ctx)
	if st.ModelID == nil {
		return nil, false
	}
	admin := User{IsAdmin: true}
	if ok, err := s.Visible(ctx, admin, *st.ModelID); err != nil || !ok {
		return nil, true
	}
	m, err := s.resolveID(ctx, admin, *st.ModelID)
	if err != nil || m.Personal {
		return nil, true
	}
	return m, false
}

// Utility 返回现在的辅助模型设置。
func (s *Service) Utility(ctx context.Context) UtilityView {
	cfg := s.cfg.LLM.Utility
	v := UtilityView{ModelID: s.utilitySetting(ctx).ModelID, Name: "配置文件里的辅助模型", Model: cfg.Model, Config: cfg.Model}
	if m, missing := s.utilityModel(ctx); m != nil {
		v.Name, v.Model = m.Name, m.Model
	} else {
		v.Missing = missing
	}
	return v
}

// SetUtility 选辅助模型（仅管理员）。id 为空表示改回配置文件里的辅助模型；只能选内置模型或公共模型。
func (s *Service) SetUtility(ctx context.Context, u User, id *uint64) (UtilityView, error) {
	if !u.IsAdmin {
		return UtilityView{}, ErrForbidden
	}
	if id != nil {
		if *id != BuiltinID {
			m, err := s.store.Models().Get(ctx, *id)
			if errors.Is(err, store.ErrNotFound) {
				return UtilityView{}, store.ErrNotFound
			}
			if err != nil {
				return UtilityView{}, err
			}
			if !m.Public() {
				return UtilityView{}, &InvalidError{"辅助模型只能选公共模型（个人模型用的是用户自己的 Key）"}
			}
		} else if !s.builtinAvailable() {
			return UtilityView{}, &InvalidError{"配置文件里的聊天模型还没有填 API Key"}
		}
	}
	b, _ := json.Marshal(utilitySetting{ModelID: id})
	if err := s.store.Meta().Set(ctx, metaUtility, string(b)); err != nil {
		return UtilityView{}, err
	}
	s.util.mu.Lock()
	s.util.at = time.Time{}
	s.util.mu.Unlock()
	return s.Utility(ctx), nil
}

// UtilityKey 是辅助模型调用的计价项：选了现有模型就按那个模型的价格计，否则按「辅助模型」的价格。
func (s *Service) UtilityKey(ctx context.Context) string {
	if m, _ := s.utilityModel(ctx); m != nil {
		return quota.ModelKey(m.ID)
	}
	return quota.KeyUtility
}

// DynamicUtility 是辅助模型：每次调用时按网页上的设置选模型，没选就用配置文件里的。
type DynamicUtility struct {
	s        *Service
	fallback llm.ChatProvider
}

// UtilityChat 返回辅助模型。fallback 是配置文件里的辅助模型。
func (s *Service) UtilityChat(fallback llm.ChatProvider) *DynamicUtility {
	return &DynamicUtility{s: s, fallback: fallback}
}

func (d *DynamicUtility) pick(ctx context.Context) llm.ChatProvider {
	if m, _ := d.s.utilityModel(ctx); m != nil && m.Chat != nil {
		return m.Chat
	}
	return d.fallback
}

// Chat 实现 llm.ChatProvider。
func (d *DynamicUtility) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return d.pick(ctx).Chat(ctx, req)
}

// ChatStream 实现 llm.ChatProvider。
func (d *DynamicUtility) ChatStream(ctx context.Context, req llm.ChatRequest) (llm.ChatStream, error) {
	return d.pick(ctx).ChatStream(ctx, req)
}

// Model 实现 llm.ChatProvider。
func (d *DynamicUtility) Model() string { return d.pick(context.Background()).Model() }
