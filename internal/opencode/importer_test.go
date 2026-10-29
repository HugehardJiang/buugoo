package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newImporter(t *testing.T) (*Importer, *billing.Service, *models.Service) {
	t.Helper()
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "o.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	box, _ := secret.New(make([]byte, 32))
	cfg := config.Default()
	cfg.Mode = config.ModeMulti
	ms, err := models.NewService(ctx, db, box, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	sh, _ := time.LoadLocation("Asia/Shanghai")
	bill := billing.New(db.Gorm(), box, sh)
	return NewImporter(db.Gorm(), db.Meta(), box, ms, bill, sh, nil), bill, ms
}

func TestImportSyncAndPersonal(t *testing.T) {
	ctx := context.Background()
	imp, bill, ms := newImporter(t)
	o := Options{Tier: "plus", Rate: 7, Markup: 1, Restricted: true} // 旧版本的档位名也认
	res, err := imp.Import(ctx, "sk-site", []string{"glm-5.2", "deepseek-v4-pro"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || res.Priced != 2 || !strings.Contains(res.PeakNote, "09:00-12:00") {
		t.Fatalf("%+v", res)
	}
	imported, _ := imp.Imported(ctx)
	glm := imported["glm-5.2"]
	if !glm.Restricted || glm.ThinkingStyle != "reasoning_effort" {
		t.Fatalf("默认仅限授权用户、可以调思考: %+v", glm)
	}
	p := bill.Pricing(ctx).Prices[quota.ModelKey(glm.ID)]
	// Go Plus：$1.40 × 40/180 × 7 ≈ 2.1778
	if p.Input != 2.1778 {
		t.Fatalf("price %+v", p)
	}

	// 官网价格变了：同步后提示，确认后才更新价格。
	page, _ := os.ReadFile("testdata/go-2026-10-29.html")
	changed := strings.Replace(string(page), "<td>GLM-5.2</td><td>$1.40</td>", "<td>GLM-5.2</td><td>$2.80</td>", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(changed)) }))
	defer srv.Close()
	imp.docsURL = srv.URL
	rep := imp.Sync(ctx)
	if rep.Error != "" || !rep.Pending || rep.Applied || len(rep.Changes) == 0 || !strings.Contains(strings.Join(rep.Changes, ";"), "GLM-5.2") {
		t.Fatalf("%+v", rep)
	}
	if imp.Catalog(ctx).Source != "synced" || len(imp.Catalog(ctx).Tiers) != 3 {
		t.Fatal("应改用同步到的目录（含 Go Teams）")
	}
	if p := bill.Pricing(ctx).Prices[quota.ModelKey(glm.ID)]; p.Input != 2.1778 {
		t.Fatalf("没确认前不改价格: %+v", p)
	}
	if err := imp.ApplyPrices(ctx); err != nil {
		t.Fatal(err)
	}
	if p := bill.Pricing(ctx).Prices[quota.ModelKey(glm.ID)]; p.Input != 4.3556 {
		t.Fatalf("应按新价格: %+v", p)
	}
	if r := imp.LastSync(ctx); r == nil || r.Pending || !r.Applied {
		t.Fatalf("%+v", r)
	}
	// 打开自动应用：再变价格时同步完自动更新。
	o.AutoApply = true
	if err := imp.SaveOptions(ctx, o); err != nil {
		t.Fatal(err)
	}
	changed = strings.Replace(string(page), "<td>GLM-5.2</td><td>$1.40</td>", "<td>GLM-5.2</td><td>$0.70</td>", 1)
	if rep := imp.Sync(ctx); !rep.Applied || rep.Pending {
		t.Fatalf("%+v", rep)
	}
	if p := bill.Pricing(ctx).Prices[quota.ModelKey(glm.ID)]; p.Input != 1.0889 {
		t.Fatalf("%+v", p)
	}
	// 官网改版、解析不出来：保留之前的目录。
	changed = "<html>改版</html>"
	if rep := imp.Sync(ctx); rep.Error == "" {
		t.Fatal("应报错")
	}
	if imp.Catalog(ctx).Source != "synced" {
		t.Fatal("继续用之前的目录")
	}

	// 普通用户用自己的 Key 一键添加（个人模型，不计费）。
	u := models.User{ID: 7}
	if _, _, err := imp.AddPersonal(ctx, u, "", []string{"kimi-k3"}); err == nil {
		t.Fatal("第一次要填 Key")
	}
	added, updated, err := imp.AddPersonal(ctx, u, "sk-mine", []string{"kimi-k3", "glm-5.3"})
	if err != nil || added != 2 || updated != 0 {
		t.Fatal(added, updated, err)
	}
	if added, updated, err = imp.AddPersonal(ctx, u, "", []string{"kimi-k3"}); err != nil || added != 0 || updated != 1 {
		t.Fatal(added, updated, err)
	}
	mine, _ := imp.Mine(ctx, u)
	if strings.Join(mine, ",") != "glm-5.3,kimi-k3" {
		t.Fatal(mine)
	}
	// 一键删除：用户删自己的，站长删公共的（连同价格）。
	if n, err := imp.Remove(ctx, u, false, []string{"glm-5.3", "glm-5.2"}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if mine, _ := imp.Mine(ctx, u); strings.Join(mine, ",") != "kimi-k3" {
		t.Fatal(mine)
	}
	if n, err := imp.Remove(ctx, admin, true, []string{"glm-5.2"}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, ok := bill.Pricing(ctx).Prices[quota.ModelKey(glm.ID)]; ok {
		t.Fatal("价格也要删掉")
	}
	if left, _ := imp.Imported(ctx); len(left) != 1 {
		t.Fatalf("%v", left)
	}
	list, _ := ms.List(ctx, u)
	for _, v := range list {
		if v.Model == "kimi-k3" && (v.Scope != models.ScopePersonal || !v.Vision || v.BaseURL != BaseURL) {
			t.Fatalf("%+v", v)
		}
	}
}

// 早期版本导入的 OpenCode 模型思考参数是「不支持」，升级后改成可以调（只改一次，管理员之后改回去不再动）。
func TestEnableThinkingForOldModels(t *testing.T) {
	ctx := context.Background()
	imp, _, ms := newImporter(t)
	v, err := ms.Create(ctx, admin, models.Input{Name: "GLM（OpenCode）", BaseURL: BaseURL, APIKey: "k", Model: "glm-5.2", ThinkingStyle: "none", Public: true})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := ms.Create(ctx, admin, models.Input{Name: "别家", BaseURL: "https://api.example.com/v1", APIKey: "k", Model: "x", ThinkingStyle: "none", Public: true})
	imp.EnableThinking(ctx)
	list, _ := ms.List(ctx, admin)
	for _, m := range list {
		if m.ID == v.ID && m.ThinkingStyle != "reasoning_effort" {
			t.Fatalf("%+v", m)
		}
		if m.ID == other.ID && m.ThinkingStyle != "none" {
			t.Fatal("别家的不动")
		}
	}
	in := models.Input{Name: "GLM（OpenCode）", BaseURL: BaseURL, Model: "glm-5.2", ThinkingStyle: "none", Public: true}
	if _, err := ms.Update(ctx, admin, v.ID, in); err != nil {
		t.Fatal(err)
	}
	imp.EnableThinking(ctx)
	list, _ = ms.List(ctx, admin)
	for _, m := range list {
		if m.ID == v.ID && m.ThinkingStyle != "none" {
			t.Fatal("只改一次")
		}
	}
}

func TestSaveAdminKey(t *testing.T) {
	ctx := context.Background()
	imp, _, _ := newImporter(t)
	// 先只保存 Key，之后添加模型时不用再填。
	if _, err := imp.SetKey(ctx, "  "); err == nil {
		t.Fatal("空 Key 应报错")
	}
	if n, err := imp.SetKey(ctx, "sk-first-key-1234"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if m := imp.KeyMask(ctx, nil); m == "" || strings.Contains(m, "first") {
		t.Fatalf("应打码: %q", m)
	}
	o := Options{Tier: TierGo, Rate: 7, Markup: 1, Restricted: true}
	if _, err := imp.Import(ctx, "", []string{"glm-5.2"}, o); err != nil {
		t.Fatalf("应沿用保存的 Key: %v", err)
	}
	imported, _ := imp.Imported(ctx)
	if !strings.HasSuffix(imported["glm-5.2"].KeyMask, "1234") {
		t.Fatalf("%+v", imported["glm-5.2"])
	}
	// 换 Key：已添加的模型一起换，名字和设置不变。
	if n, err := imp.SetKey(ctx, "sk-second-key-9876"); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	imported, _ = imp.Imported(ctx)
	g := imported["glm-5.2"]
	if !strings.HasSuffix(g.KeyMask, "9876") || !g.Restricted || g.Name != "GLM-5.2（OpenCode）" {
		t.Fatalf("%+v", g)
	}
}
