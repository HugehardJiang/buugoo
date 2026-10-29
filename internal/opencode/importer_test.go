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
	return NewImporter(db.Gorm(), db.Meta(), ms, bill, sh, nil), bill, ms
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
	if !glm.Restricted {
		t.Fatal("默认仅限授权用户")
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
	list, _ := ms.List(ctx, u)
	for _, v := range list {
		if v.Model == "kimi-k3" && (v.Scope != models.ScopePersonal || !v.Vision || v.BaseURL != BaseURL) {
			t.Fatalf("%+v", v)
		}
	}
}
