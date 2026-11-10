package models

import (
	"context"
	"errors"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 批量修改：在设置页多选模型，一次改同一项设置（例如都改成能看图、都设成仅限授权用户）或者一起删掉。

// MaxBatch 是一次最多改多少个模型。
const MaxBatch = 200

// Patch 是批量修改的内容，为空的字段表示这一项不改。
type Patch struct {
	ThinkingStyle *string  `json:"thinking_style,omitempty"`
	ThinkingModel *string  `json:"thinking_model,omitempty"`
	Vision        *bool    `json:"vision,omitempty"`
	Tools         *bool    `json:"tools,omitempty"`
	Restricted    *bool    `json:"restricted,omitempty"`
	ContextTokens *int     `json:"context_tokens,omitempty"`
	MaxTokens     *int     `json:"max_tokens,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
}

func (p Patch) empty() bool {
	return p.ThinkingStyle == nil && p.ThinkingModel == nil && p.Vision == nil && p.Tools == nil && p.Restricted == nil &&
		p.ContextTokens == nil && p.MaxTokens == nil && p.Temperature == nil
}

// BatchFailure 是没改成的一个模型和原因。
type BatchFailure struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// BatchResult 是批量操作的结果。
type BatchResult struct {
	Done   int            `json:"done"`
	Failed []BatchFailure `json:"failed"`
}

func checkBatch(ids []uint64) error {
	if len(ids) == 0 {
		return &InvalidError{"至少选一个模型"}
	}
	if len(ids) > MaxBatch {
		return &InvalidError{"一次最多选 200 个模型"}
	}
	return nil
}

// failure 把错误换成给人看的原因（别人的模型一律说找不到，不暴露是否存在）。
func failure(id uint64, name string, err error) BatchFailure {
	msg := err.Error()
	var inv *InvalidError
	switch {
	case errors.Is(err, store.ErrNotFound):
		msg = "找不到这个模型"
	case errors.Is(err, ErrForbidden):
		msg = "没有权限修改"
	case errors.As(err, &inv):
		msg = inv.Msg
	}
	return BatchFailure{ID: id, Name: name, Error: msg}
}

// BatchUpdate 把同一组修改应用到多个模型上，每个模型照常校验（例如「换用推理模型」要有推理模型名），
// 改不了的记下原因，其余的照改。
func (s *Service) BatchUpdate(ctx context.Context, u User, ids []uint64, p Patch) (BatchResult, error) {
	res := BatchResult{Failed: []BatchFailure{}}
	if err := checkBatch(ids); err != nil {
		return res, err
	}
	if p.empty() {
		return res, &InvalidError{"没有选要改哪一项"}
	}
	for _, id := range ids {
		m, err := s.editable(ctx, u, id)
		if err != nil {
			res.Failed = append(res.Failed, failure(id, "", err))
			continue
		}
		in := Input{Name: m.Name, BaseURL: m.BaseURL, Model: m.Model, ThinkingStyle: m.ThinkingStyle, ThinkingModel: m.ThinkingModel,
			Temperature: m.Temperature, MaxTokens: m.MaxTokens, Vision: m.Vision, Tools: m.Tools, ContextTokens: m.ContextTokens,
			Public: m.Public(), Restricted: m.Restricted}
		if p.ThinkingStyle != nil {
			in.ThinkingStyle = *p.ThinkingStyle
		}
		if p.ThinkingModel != nil {
			in.ThinkingModel = *p.ThinkingModel
		}
		if p.Vision != nil {
			in.Vision = *p.Vision
		}
		if p.Tools != nil {
			in.Tools = *p.Tools
		}
		if p.Restricted != nil {
			in.Restricted = *p.Restricted
		}
		if p.ContextTokens != nil {
			in.ContextTokens = *p.ContextTokens
		}
		if p.MaxTokens != nil {
			in.MaxTokens = *p.MaxTokens
		}
		if p.Temperature != nil {
			in.Temperature = *p.Temperature
		}
		if _, err := s.Update(ctx, u, id, in); err != nil {
			res.Failed = append(res.Failed, failure(id, m.Name, err))
			continue
		}
		res.Done++
	}
	return res, nil
}

// BatchDelete 一次删除多个模型（用它们的对话改回默认模型）。
func (s *Service) BatchDelete(ctx context.Context, u User, ids []uint64) (BatchResult, error) {
	res := BatchResult{Failed: []BatchFailure{}}
	if err := checkBatch(ids); err != nil {
		return res, err
	}
	for _, id := range ids {
		if err := s.Delete(ctx, u, id); err != nil {
			res.Failed = append(res.Failed, failure(id, "", err))
			continue
		}
		res.Done++
	}
	return res, nil
}
