package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

type fakeChat struct {
	cfg        config.ModelConfig
	publicOnly bool
}

func (f *fakeChat) Model() string { return f.cfg.Model }
func (f *fakeChat) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: "你好呀"}, nil
}
func (f *fakeChat) ChatStream(context.Context, llm.ChatRequest) (llm.ChatStream, error) {
	return nil, errors.New("not implemented")
}

func newService(t *testing.T, mode, builtinKey string) (*Service, *gormstore.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "m.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Mode = mode
	cfg.LLM.Chat.APIKey = builtinKey
	svc, err := NewService(ctx, db, box, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.newChat = func(c config.ModelConfig, publicOnly bool) llm.ChatProvider {
		return &fakeChat{cfg: c, publicOnly: publicOnly}
	}
	return svc, db
}

func input(name, key string) Input {
	return Input{Name: name, BaseURL: "https://api.example.com/v1/", APIKey: key, Model: "m-" + name}
}

func TestPublicModelTestsRequireAdmin(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, config.ModeMulti, "builtin-key")
	admin, user := User{ID: 1, IsAdmin: true}, User{ID: 2}
	in := input("public", "public-key")
	in.Public = true
	pub, err := svc.Create(ctx, admin, in)
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.Create(ctx, user, input("personal", "personal-key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{BuiltinID, pub.ID} {
		if _, err := svc.Test(ctx, user, id, nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("ordinary user testing shared model %d: %v", id, err)
		}
		if res, err := svc.Test(ctx, admin, id, nil); err != nil || !res.OK {
			t.Fatalf("admin testing shared model %d: %+v %v", id, res, err)
		}
	}
	if res, err := svc.Test(ctx, user, personal.ID, nil); err != nil || !res.OK {
		t.Fatalf("personal model test: %+v %v", res, err)
	}
	custom := input("custom", "own-key")
	if res, err := svc.Test(ctx, user, 0, &custom); err != nil || !res.OK {
		t.Fatalf("own supplied key test: %+v %v", res, err)
	}
}

func TestPersonalAndPublicModels(t *testing.T) {
	ctx := context.Background()
	svc, db := newService(t, config.ModeMulti, "builtin-key")
	admin, alice, bob := User{ID: 1, IsAdmin: true}, User{ID: 2}, User{ID: 3}

	if _, err := svc.Create(ctx, alice, Input{Name: "x", BaseURL: "ftp://a", APIKey: "k", Model: "m"}); err == nil {
		t.Fatal("应校验 Base URL")
	}
	in := input("pub", "sk-public-1234567890")
	in.Public = true
	if _, err := svc.Create(ctx, alice, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("普通用户不能添加公共模型: %v", err)
	}
	pub, err := svc.Create(ctx, admin, in)
	if err != nil || pub.Scope != ScopePublic || pub.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("pub=%+v err=%v", pub, err)
	}
	mine, err := svc.Create(ctx, alice, input("mine", "sk-alice-secret-abcdef"))
	if err != nil || mine.Scope != ScopePersonal || !mine.Editable {
		t.Fatalf("mine=%+v err=%v", mine, err)
	}
	if strings.Contains(mine.KeyMask, "secret") {
		t.Fatalf("Key 应脱敏: %q", mine.KeyMask)
	}

	// 数据库中只保存密文。
	raw, err := db.Models().Get(ctx, mine.ID)
	if err != nil || strings.Contains(raw.APIKey, "sk-alice") || !strings.HasPrefix(raw.APIKey, "v1:") {
		t.Fatalf("Key 应加密保存: %q", raw.APIKey)
	}

	// 可见性：alice 看到内置 + 公共 + 自己的；bob 看不到 alice 的。
	la, _ := svc.List(ctx, alice)
	lb, _ := svc.List(ctx, bob)
	if len(la) != 3 || len(lb) != 2 {
		t.Fatalf("alice=%d bob=%d", len(la), len(lb))
	}
	if ok, _ := svc.Visible(ctx, bob, mine.ID); ok {
		t.Fatal("他人的模型不可见")
	}
	if _, err := svc.Update(ctx, bob, mine.ID, input("hack", "")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("不能修改他人的模型: %v", err)
	}
	if _, err := svc.Update(ctx, alice, pub.ID, input("hack", "")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("普通用户不能修改公共模型: %v", err)
	}
	if _, err := svc.Test(ctx, bob, mine.ID, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("不能测试他人的模型: %v", err)
	}

	// 默认模型：没有设置时用内置；设置个人默认后用个人的。
	r, err := svc.Resolve(ctx, alice, nil)
	if err != nil || r.ID != BuiltinID || r.Personal {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if err := svc.SetDefault(ctx, alice, mine.ID); err != nil {
		t.Fatal(err)
	}
	r, err = svc.Resolve(ctx, alice, nil)
	if err != nil || r.ID != mine.ID || !r.Personal {
		t.Fatalf("应使用个人默认模型: %+v %v", r, err)
	}
	fc := r.Chat.(*fakeChat)
	if fc.cfg.APIKey != "sk-alice-secret-abcdef" || !fc.publicOnly {
		t.Fatalf("应解密 Key，且普通用户的模型只能连公网: %+v", fc)
	}
	// bob 的默认不受影响；bob 指定 alice 的模型时退回默认。
	r, _ = svc.Resolve(ctx, bob, &mine.ID)
	if r.ID != BuiltinID {
		t.Fatalf("无权使用的模型应退回默认: %+v", r)
	}
	r, _ = svc.Resolve(ctx, bob, &pub.ID)
	if r.ID != pub.ID || r.Personal || r.Chat.(*fakeChat).publicOnly {
		t.Fatalf("公共模型计入限额、不限制地址: %+v", r)
	}

	// 修改时 Key 留空表示保持不变。
	upd := input("mine2", "")
	if _, err := svc.Update(ctx, alice, mine.ID, upd); err != nil {
		t.Fatal(err)
	}
	r, _ = svc.Resolve(ctx, alice, &mine.ID)
	if r.Name != "mine2" || r.Chat.(*fakeChat).cfg.APIKey != "sk-alice-secret-abcdef" {
		t.Fatalf("r=%+v", r)
	}

	// 改回站点默认；管理员设置公共默认后所有人生效，再设回配置文件模型。
	if err := svc.ClearDefault(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if r, _ = svc.Resolve(ctx, alice, nil); r.ID != BuiltinID {
		t.Fatalf("取消个人默认后应使用站点默认: %+v", r)
	}
	if err := svc.SetDefault(ctx, alice, BuiltinID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("普通用户不能设置站点默认: %v", err)
	}
	if err := svc.SetDefault(ctx, admin, pub.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ = svc.Resolve(ctx, bob, nil); r.ID != pub.ID {
		t.Fatalf("公共默认应对所有人生效: %+v", r)
	}
	if err := svc.SetDefault(ctx, admin, BuiltinID); err != nil {
		t.Fatal(err)
	}
	if r, _ = svc.Resolve(ctx, bob, nil); r.ID != BuiltinID {
		t.Fatalf("r=%+v", r)
	}

	// 删除模型后，使用它的对话改回默认。
	conv := &store.Conversation{UserID: alice.ID, ModelID: &mine.ID, Thinking: store.ThinkingOff}
	if err := db.Conversations().Create(ctx, conv); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, alice, mine.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := db.Conversations().Get(ctx, alice.ID, conv.ID)
	if got.ModelID != nil {
		t.Fatal("删除模型后对话应改回默认模型")
	}
}

func TestSingleModeAndNoModel(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, config.ModeSingle, "")
	owner := User{ID: 1, IsAdmin: true}
	if _, err := svc.Resolve(ctx, owner, nil); !errors.Is(err, ErrNoModel) {
		t.Fatalf("没有模型时应报 ErrNoModel: %v", err)
	}
	if ms, _ := svc.List(ctx, owner); len(ms) != 0 {
		t.Fatalf("内置模型没有 Key 时不应列出: %+v", ms)
	}
	v, err := svc.Create(ctx, owner, input("ds", "sk-1"))
	if err != nil || v.Scope != ScopePublic {
		t.Fatalf("single 模式下添加的模型为公共模型: %+v %v", v, err)
	}
	r, err := svc.Resolve(ctx, owner, nil)
	if err != nil || r.ID != v.ID || r.Personal || r.Chat.(*fakeChat).publicOnly {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	res, err := svc.Test(ctx, owner, 0, &Input{Name: "t", BaseURL: "http://localhost:11434/v1", APIKey: "k", Model: "qwen"})
	if err != nil || !res.OK || res.Reply != "你好呀" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestEmbeddingSettings(t *testing.T) {
	ctx := context.Background()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}],"usage":{"prompt_tokens":1}}`))
	}))
	defer srv.Close()

	svc, _ := newService(t, config.ModeMulti, "")
	if svc.EmbeddingReady(ctx) {
		t.Fatal("配置文件没有 Key 时不应就绪")
	}
	if _, _, err := svc.SetEmbedding(ctx, User{ID: 2}, EmbeddingInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatal("只有管理员能修改向量模型")
	}
	admin := User{ID: 1, IsAdmin: true}
	in := EmbeddingInput{BaseURL: srv.URL, APIKey: "sk-emb", Model: "test-emb", Dim: 3}
	res, err := svc.TestEmbedding(ctx, admin, &in)
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	v, changed, err := svc.SetEmbedding(ctx, admin, in)
	if err != nil || !changed || v.Source != "web" || !v.Ready {
		t.Fatalf("v=%+v changed=%v err=%v", v, changed, err)
	}
	if svc.Embedding().Model() != "test-emb" {
		t.Fatal("保存后应立即切换向量模型")
	}
	if _, err := svc.Embedding().Embed(ctx, []string{"布谷"}); err != nil || gotAuth != "Bearer sk-emb" {
		t.Fatalf("err=%v auth=%q", err, gotAuth)
	}
	// Key 留空保持不变，模型不变时 changed 为 false。
	in.APIKey = ""
	if _, changed, err := svc.SetEmbedding(ctx, admin, in); err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}

func TestContextDetect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"other","context_length":1},{"id":"m1","top_provider":{"context_length":262144}},{"id":"m2"}]}`))
	}))
	defer srv.Close()
	c := newContextCache()
	ctx := context.Background()
	if got := c.detect(ctx, srv.URL+"/v1", "k", "m1", false); got.Tokens != 262144 || got.Source != ContextAPI {
		t.Fatalf("%+v", got)
	}
	if got := c.detect(ctx, srv.URL+"/v1", "k", "deepseek-flash", false); got.Tokens != 128000 || got.Source != ContextTable {
		t.Fatalf("接口里没有时查常见模型表 %+v", got)
	}
	if got := c.detect(ctx, srv.URL+"/v1", "k", "moonshot-v1-32k", false); got.Tokens != 32*1024 {
		t.Fatalf("模型名里的 32k %+v", got)
	}
	if got := c.detect(ctx, srv.URL+"/v1", "k", "m2", false); got.Tokens != DefaultContextTokens || got.Source != ContextDefault {
		t.Fatalf("%+v", got)
	}
	// 结果会缓存，不会每次都请求接口。
	calls := 0
	c.fetch = func(context.Context, string, string, string, bool) int { calls++; return 0 }
	c.detect(ctx, srv.URL+"/v1", "k", "m1", false)
	if calls != 0 {
		t.Fatal("应使用缓存")
	}
}

// 仅限授权用户的公共模型：没授权的看不到、用不了，设为全站默认时也退回别的模型。
func TestRestrictedModels(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, config.ModeMulti, "builtin-key")
	admin, plain, vip := User{ID: 1, IsAdmin: true}, User{ID: 2}, User{ID: 3, Restricted: true}
	in := input("opencode", "oc-key")
	in.Public, in.Restricted, in.Vision = true, true, true
	m, err := svc.Create(ctx, admin, in)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Restricted {
		t.Fatal("应标为仅限授权用户")
	}
	if err := svc.SetDefault(ctx, admin, m.ID); err != nil {
		t.Fatal(err)
	}
	has := func(u User) bool {
		list, err := svc.List(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range list {
			if v.ID == m.ID {
				return true
			}
		}
		return false
	}
	if has(plain) || !has(vip) || !has(admin) {
		t.Fatalf("plain=%v vip=%v admin=%v", has(plain), has(vip), has(admin))
	}
	if ok, _ := svc.Visible(ctx, plain, m.ID); ok {
		t.Fatal("没授权不能用")
	}
	id := m.ID
	r, err := svc.Resolve(ctx, plain, &id)
	if err != nil || r.ID == m.ID {
		t.Fatalf("没授权的用户应退回默认模型: %+v %v", r, err)
	}
	if r, err := svc.Resolve(ctx, vip, nil); err != nil || r.ID != m.ID {
		t.Fatalf("授权用户用全站默认: %+v %v", r, err)
	}
	// 修改时也能改看图、上下文和授权范围（之前修改不保存这几项）。
	up := input("opencode", "")
	up.Vision, up.ContextTokens, up.Restricted = false, 200000, false
	if _, err := svc.Update(ctx, admin, m.ID, up); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.List(ctx, plain)
	for _, v := range list {
		if v.ID == m.ID && (v.Vision || v.ContextTokens != 200000 || v.Restricted) {
			t.Fatalf("修改没保存: %+v", v)
		}
	}
	if !has(plain) {
		t.Fatal("取消限制后所有人能用")
	}
}

// 「最大输出 0（不限）」、温度 0、关掉工具都要原样存下来（以前会被数据库默认值换成 1024、0.8、开着）。
func TestZeroValuesKept(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, config.ModeMulti, "builtin-key")
	alice := User{ID: 2}
	in := input("zero", "k")
	in.MaxTokens, in.Temperature, in.Tools = 0, 0, false
	v, err := svc.Create(ctx, alice, in)
	if err != nil {
		t.Fatal(err)
	}
	if v.MaxTokens != 0 || v.Temperature != 0 || v.Tools {
		t.Fatalf("%+v", v)
	}
	list, _ := svc.List(ctx, alice)
	for _, m := range list {
		if m.ID == v.ID && (m.MaxTokens != 0 || m.Temperature != 0 || m.Tools) {
			t.Fatalf("读回来变了: %+v", m)
		}
	}
	in.MaxTokens = 1_000_000
	if _, err := svc.Update(ctx, alice, v.ID, in); err != nil {
		t.Fatalf("应能填很长的最大输出: %v", err)
	}
	in.MaxTokens = MaxOutputTokens + 1
	if _, err := svc.Update(ctx, alice, v.ID, in); err == nil {
		t.Fatal("超过上限应报错")
	}
}
