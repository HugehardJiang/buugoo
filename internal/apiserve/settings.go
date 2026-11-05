package apiserve

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/store"
)

const metaSettings = "api.settings"

// DefaultScale 是没单独设置时 API 调用的价格倍率：比网页贵一些，引导大家用网页。
const DefaultScale = 1.5

// MaxScale 是倍率上限。
const MaxScale = 100

// Settings 是 API 接入的价格倍率。只对管理员添加的模型（内置和公共模型）生效；
// 用户自己添加的模型用的是自己的 Key，不计费也不乘倍率。
type Settings struct {
	// Default 是默认倍率。
	Default float64 `json:"default"`
	// Models 是单独设置的模型倍率（模型编号 → 倍率），0 号是配置文件里的模型。
	Models map[string]float64 `json:"models"`
}

// Scale 返回某个模型的倍率。
func (s Settings) Scale(modelID uint64) float64 {
	if v, ok := s.Models[strconv.FormatUint(modelID, 10)]; ok && v > 0 {
		return v
	}
	if s.Default > 0 {
		return s.Default
	}
	return DefaultScale
}

// SettingsStore 读写倍率设置（带 30 秒缓存，每次 API 调用都要查）。
type SettingsStore struct {
	meta store.MetaRepo
	mu   sync.Mutex
	at   time.Time
	cur  Settings
}

// NewSettingsStore 创建 SettingsStore。
func NewSettingsStore(meta store.MetaRepo) *SettingsStore { return &SettingsStore{meta: meta} }

// Get 返回现在的设置。
func (s *SettingsStore) Get(ctx context.Context) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.at.IsZero() && time.Since(s.at) < 30*time.Second {
		return s.cur
	}
	st := Settings{Default: DefaultScale}
	if raw, ok, err := s.meta.Get(ctx, metaSettings); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	if st.Default <= 0 {
		st.Default = DefaultScale
	}
	if st.Models == nil {
		st.Models = map[string]float64{}
	}
	s.cur, s.at = st, time.Now()
	return st
}

// Set 保存设置。
func (s *SettingsStore) Set(ctx context.Context, st Settings) (Settings, error) {
	if st.Default <= 0 || st.Default > MaxScale {
		return Settings{}, &UserError{fmt.Sprintf("默认倍率要大于 0，最多 %d", MaxScale)}
	}
	clean := map[string]float64{}
	for k, v := range st.Models {
		if _, err := strconv.ParseUint(k, 10, 64); err != nil {
			return Settings{}, &UserError{"模型编号不对"}
		}
		if v == 0 {
			continue // 0 表示用默认倍率
		}
		if v < 0 || v > MaxScale {
			return Settings{}, &UserError{fmt.Sprintf("倍率要大于 0，最多 %d", MaxScale)}
		}
		clean[k] = v
	}
	st.Models = clean
	b, _ := json.Marshal(st)
	if err := s.meta.Set(ctx, metaSettings, string(b)); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	s.cur, s.at = st, time.Now()
	s.mu.Unlock()
	return st, nil
}
