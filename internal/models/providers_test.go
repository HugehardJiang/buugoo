package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

func TestProviders(t *testing.T) {
	ctx := context.Background()
	svc, db := newService(t, config.ModeMulti, "")
	admin, alice, bob := User{ID: 1, IsAdmin: true}, User{ID: 2}, User{ID: 3}

	// 升级前的模型：同一个人、同一个地址和 Key 的归到一起，Key 不同的分开。
	in1, in2, in3 := input("a", "k1"), input("b", "k1"), input("c", "k2")
	for _, in := range []Input{in1, in2, in3} {
		if _, err := svc.Create(ctx, alice, in); err != nil {
			t.Fatal(err)
		}
	}
	// 模拟升级前：去掉供应商，再按启动时的逻辑归组。
	db.Gorm().Exec("UPDATE model_configs SET provider_id = NULL")
	db.Gorm().Exec("DELETE FROM model_providers")
	if n, err := svc.GroupModels(ctx); err != nil || n != 3 {
		t.Fatalf("归组 %d %v", n, err)
	}
	ps, _ := svc.Providers(ctx, alice)
	if len(ps) != 2 || ps[0].Name != "example.com" || ps[1].Name != "example.com 2" {
		t.Fatalf("应分成两个供应商：%+v", ps)
	}
	list, _ := svc.List(ctx, alice)
	if list[0].ProviderID != list[1].ProviderID || list[0].ProviderID == list[2].ProviderID || list[0].Provider != "example.com" {
		t.Fatalf("模型的供应商不对：%+v", list)
	}
	if n, _ := svc.GroupModels(ctx); n != 0 {
		t.Fatal("重复归组应该什么也不做")
	}

	// 供应商的 /models：列出模型，标出已添加的，聊天模型排前面。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer pk" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "text-embedding-3"}, {"id": "qwen-vl-max", "context_length": 131072}, {"id": "glm-5"}, {"id": "glm-5"}}})
	}))
	defer srv.Close()
	p, err := svc.CreateProvider(ctx, admin, ProviderInput{BaseURL: srv.URL + "/v1/", APIKey: "pk", Public: true})
	if err != nil || p.Scope != ScopePublic || p.Name == "" || p.KeyMask == "" {
		t.Fatalf("新建供应商：%+v %v", p, err)
	}
	if _, err := svc.CreateProvider(ctx, alice, ProviderInput{Name: "x", BaseURL: srv.URL, APIKey: "k", Public: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("普通用户不能建公共供应商：%v", err)
	}
	if _, err := svc.RemoteModels(ctx, alice, p.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("普通用户不能读公共供应商的模型列表：%v", err)
	}
	rm, err := svc.RemoteModels(ctx, admin, p.ID)
	if err != nil || len(rm) != 3 || rm[0].ID != "glm-5" || !rm[1].Vision || rm[1].ContextTokens != 131072 || rm[2].Chat {
		t.Fatalf("模型列表：%+v %v", rm, err)
	}
	n, err := svc.AddModels(ctx, admin, p.ID, []AddItem{{Model: "glm-5", Tools: true}, {Model: "qwen-vl-max", Name: "千问 VL", Vision: true}}, true)
	if err != nil || n != 2 {
		t.Fatalf("批量添加 %d %v", n, err)
	}
	if n, _ := svc.AddModels(ctx, admin, p.ID, []AddItem{{Model: "glm-5"}}, false); n != 0 {
		t.Fatal("已经有的应跳过")
	}
	rm, _ = svc.RemoteModels(ctx, admin, p.ID)
	if !rm[0].Added {
		t.Fatalf("应标出已添加：%+v", rm)
	}
	// 新加的模型用供应商的地址和 Key，是公共的、仅限授权用户。
	pub, _ := svc.List(ctx, admin)
	var glm View
	for _, v := range pub {
		if v.Model == "glm-5" {
			glm = v
		}
	}
	if glm.ProviderID != p.ID || glm.Scope != ScopePublic || !glm.Restricted || glm.BaseURL != srv.URL+"/v1" {
		t.Fatalf("新模型：%+v", glm)
	}
	// 普通用户：没授权时看不到仅限授权的模型，也就看不到这个供应商。
	if ps, _ := svc.Providers(ctx, bob); len(ps) != 0 {
		t.Fatalf("bob 不应看到供应商：%+v", ps)
	}

	// 改供应商的 Key：下面的模型一起换。
	if _, err := svc.UpdateProvider(ctx, admin, p.ID, ProviderInput{Name: "我的中转", BaseURL: srv.URL + "/v1", APIKey: "pk2"}); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Resolve(ctx, admin, &glm.ID)
	if err != nil || r.APIKey != "pk2" {
		t.Fatalf("模型应换成新 Key：%+v %v", r, err)
	}
	// 编辑模型但不动地址和 Key：还在原来的供应商下面。
	if _, err := svc.Update(ctx, admin, glm.ID, Input{Name: "GLM-5", Model: "glm-5", Tools: true, Public: true}); err != nil {
		t.Fatal(err)
	}
	if m, _ := db.Models().Get(ctx, glm.ID); m.ProviderID == nil || *m.ProviderID != p.ID || m.Name != "GLM-5" {
		t.Fatalf("编辑后：%+v", m)
	}
	// 普通用户改不了公共供应商。
	if _, err := svc.UpdateProvider(ctx, alice, p.ID, ProviderInput{Name: "x", BaseURL: srv.URL}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("%v", err)
	}
	// 删除供应商连同模型一起删。
	if n, err := svc.DeleteProvider(ctx, admin, p.ID); err != nil || n != 2 {
		t.Fatalf("删除 %d %v", n, err)
	}
	if _, err := db.Providers().Get(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("供应商应已删除")
	}
}

// 不指定供应商添加模型（例如 OpenCode 一键添加）：自动归到同样地址和 Key 的供应商下面；换 Key 后换到新的供应商，旧的空了就删掉。
func TestCreateAttachesProvider(t *testing.T) {
	ctx := context.Background()
	svc, db := newService(t, config.ModeMulti, "")
	u := User{ID: 2}
	a, _ := svc.Create(ctx, u, Input{Name: "A", BaseURL: "https://api.deepseek.com/v1", APIKey: "k", Model: "deepseek-flash"})
	b, _ := svc.Create(ctx, u, Input{Name: "B", BaseURL: "https://api.deepseek.com/v1", APIKey: "k", Model: "deepseek-v4-pro"})
	if a.ProviderID == 0 || a.ProviderID != b.ProviderID {
		t.Fatalf("应归到同一个供应商：%d %d", a.ProviderID, b.ProviderID)
	}
	ps, _ := svc.Providers(ctx, u)
	if len(ps) != 1 || ps[0].Name != "DeepSeek" {
		t.Fatalf("%+v", ps)
	}
	for _, id := range []uint64{a.ID, b.ID} {
		m, _ := db.Models().Get(ctx, id)
		if _, err := svc.Update(ctx, u, id, Input{Name: m.Name, BaseURL: m.BaseURL, APIKey: "k2", Model: m.Model}); err != nil {
			t.Fatal(err)
		}
	}
	ps, _ = svc.Providers(ctx, u)
	if len(ps) != 1 || ps[0].ID == a.ProviderID {
		t.Fatalf("换 Key 后应只剩新的供应商：%+v", ps)
	}
}

// Claude 经中转站接入：有「-thinking」版本的开思考时换用它，没有的用 reasoning_effort。
func TestRemoteClaudeThinking(t *testing.T) {
	ctx := context.Background()
	svc, db := newService(t, config.ModeMulti, "")
	admin := User{ID: 1, IsAdmin: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "claude-opus-4-6"}, {"id": "claude-opus-4-6-thinking"}, {"id": "claude-opus-5-5"}, {"id": "some-chat-model"}}})
	}))
	defer srv.Close()
	p, err := svc.CreateProvider(ctx, admin, ProviderInput{Name: "中转", BaseURL: srv.URL + "/v1", APIKey: "k", Public: true})
	if err != nil {
		t.Fatal(err)
	}
	rm, err := svc.RemoteModels(ctx, admin, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]RemoteModel{}
	for _, m := range rm {
		got[m.ID] = m
	}
	if m := got["claude-opus-4-6"]; m.ThinkingStyle != store.ThinkingStyleSwitchMod || m.ThinkingModel != "claude-opus-4-6-thinking" {
		t.Fatalf("有 -thinking 版本：%+v", m)
	}
	if m := got["claude-opus-5-5"]; m.ThinkingStyle != store.ThinkingStyleEffort || m.ThinkingModel != "" {
		t.Fatalf("没有 -thinking 版本：%+v", m)
	}
	if m := got["some-chat-model"]; m.ThinkingStyle != store.ThinkingStyleNone {
		t.Fatalf("认不出的模型不开思考：%+v", m)
	}
	o := got["claude-opus-4-6"]
	if _, err := svc.AddModels(ctx, admin, p.ID, []AddItem{{Model: o.ID, Tools: true, ThinkingStyle: o.ThinkingStyle, ThinkingModel: o.ThinkingModel}}, false); err != nil {
		t.Fatal(err)
	}
	ms, _ := db.Models().ListByProvider(ctx, p.ID)
	if len(ms) != 1 || ms[0].ThinkingStyle != store.ThinkingStyleSwitchMod || ms[0].ThinkingModel != "claude-opus-4-6-thinking" {
		t.Fatalf("添加后：%+v", ms)
	}
}
