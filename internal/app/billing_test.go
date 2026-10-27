package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
)

// 会员、卡密和用户管理的接口，重点是安全：默认管理员密码不能用、卡密只能兑换一次、输错多次会锁。
func TestBillingAPI(t *testing.T) {
	ta := newTestApp(t, func(c *config.Config) {
		multiConfig(c)
		c.Admin.Password = "strong-admin-pass"
		c.Quota.DailyMessages = 1
	})
	if os.Getenv("BUUGOO_TEST_MYSQL_DSN") != "" {
		resetMySQL(t, ta)
	}
	ctx := context.Background()
	if _, err := ta.Auth.CreateUser(ctx, "admin", "adminpass", true); err != nil {
		t.Fatal(err)
	}
	alice, err := ta.Auth.CreateUser(ctx, "alice", "password1", false)
	if err != nil {
		t.Fatal(err)
	}
	login := func(name, pw string) string {
		resp, b := ta.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": name, "password": pw})
		if resp.StatusCode != 200 {
			t.Fatalf("login %s: %d %s", name, resp.StatusCode, b)
		}
		var s struct{ Token string }
		_ = json.Unmarshal(b, &s)
		return s.Token
	}
	tok := login("alice", "password1")

	_, b := ta.do(t, "POST", "/api/v1/console/login", "", map[string]string{"password": "strong-admin-pass"})
	var cl struct{ Token string }
	_ = json.Unmarshal(b, &cl)
	console := func(method, path string, body any) (*http.Response, []byte) {
		var r io.Reader
		if body != nil {
			bb, _ := json.Marshal(body)
			r = bytes.NewReader(bb)
		}
		req, _ := http.NewRequest(method, ta.srv.URL+path, r)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Console-Token", cl.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp, out
	}

	// 没登录管理后台、普通用户的令牌都不行。
	if resp, _ := ta.do(t, "GET", "/api/v1/console/plans", tok, nil); resp.StatusCode != 401 {
		t.Fatalf("普通用户访问套餐管理: %d", resp.StatusCode)
	}

	resp, b := console("POST", "/api/v1/console/plans", map[string]any{"name": "月卡", "price": "¥9.9", "kind": "period", "unit": "messages",
		"days": 30, "cycle": "day", "quota": 3, "visible": true, "buy_url": "https://shop.example.com/m"})
	if resp.StatusCode != 200 {
		t.Fatalf("create plan: %d %s", resp.StatusCode, b)
	}
	var plan struct{ ID uint64 }
	_ = json.Unmarshal(b, &plan)
	resp, b = console("POST", "/api/v1/console/cards/generate", map[string]any{"plan_id": plan.ID, "count": 2, "prefix": "BG", "note": "测试"})
	if resp.StatusCode != 200 {
		t.Fatalf("generate: %d %s", resp.StatusCode, b)
	}
	var gen struct{ Codes []string }
	_ = json.Unmarshal(b, &gen)
	// 列表和导出里都没有原文。
	_, list := console("GET", "/api/v1/console/cards", nil)
	_, csv := console("GET", "/api/v1/console/cards/export", nil)
	body := strings.ReplaceAll(gen.Codes[0], "-", "")[2:]
	if strings.Contains(string(list), body[:8]) || strings.Contains(string(csv), body[:8]) {
		t.Fatal("卡密原文不应出现在列表或导出里")
	}
	if strings.Contains(string(list), "CodeHash") || strings.Contains(string(list), "code_hash") || !strings.Contains(string(list), `"hint"`) {
		t.Fatalf("卡密列表: %s", list)
	}
	// 新建时就隐藏的套餐，用户看不到。
	console("POST", "/api/v1/console/plans", map[string]any{"name": "内部套餐", "kind": "pack", "unit": "tokens", "quota": 100, "visible": false})
	_, b = console("GET", "/api/v1/console/plans", nil)
	if !strings.Contains(string(b), `"name":"内部套餐"`) || !strings.Contains(string(b), `"visible":false`) {
		t.Fatalf("console plans: %s", b)
	}

	// 用户看到套餐，兑换后变成会员，额度变成每天 3 条。
	_, b = ta.do(t, "GET", "/api/v1/billing", tok, nil)
	if !strings.Contains(string(b), "月卡") || !strings.Contains(string(b), "shop.example.com") || strings.Contains(string(b), "内部套餐") {
		t.Fatalf("billing: %s", b)
	}
	resp, b = ta.do(t, "POST", "/api/v1/billing/redeem", tok, map[string]string{"code": gen.Codes[0]})
	if resp.StatusCode != 200 {
		t.Fatalf("redeem: %d %s", resp.StatusCode, b)
	}
	if resp, _ := ta.do(t, "POST", "/api/v1/billing/redeem", tok, map[string]string{"code": gen.Codes[0]}); resp.StatusCode != 400 {
		t.Fatalf("重复兑换: %d", resp.StatusCode)
	}
	_, b = ta.do(t, "GET", "/api/v1/me", tok, nil)
	if !strings.Contains(string(b), `"messages_limit":3`) {
		t.Fatalf("会员额度: %s", b)
	}

	// 连续输错 8 次就锁住，正确的卡密也暂时兑换不了。
	for i := 0; i < 8; i++ {
		ta.do(t, "POST", "/api/v1/billing/redeem", tok, map[string]string{"code": "BG-AAAA-BBBB-CCCC-DDDD"})
	}
	if resp, b := ta.do(t, "POST", "/api/v1/billing/redeem", tok, map[string]string{"code": gen.Codes[1]}); resp.StatusCode != 429 {
		t.Fatalf("输错多次后应锁住: %d %s", resp.StatusCode, b)
	}

	// 用户管理：列表、停用（马上不能用）、恢复、重置密码、删除。
	_, b = console("GET", "/api/v1/console/users?q=ali", nil)
	if !strings.Contains(string(b), `"plan_name":"月卡"`) {
		t.Fatalf("users: %s", b)
	}
	id := strings.TrimSpace(strings.Split(strings.Split(string(b), `"id":`)[1], ",")[0])
	if resp, b := console("POST", "/api/v1/console/users/"+id+"/disable", map[string]any{"disabled": true, "reason": "测试"}); resp.StatusCode != 204 {
		t.Fatalf("disable: %d %s", resp.StatusCode, b)
	}
	if resp, _ := ta.do(t, "GET", "/api/v1/me", tok, nil); resp.StatusCode != 401 && resp.StatusCode != 403 {
		t.Fatalf("停用后旧令牌应失效: %d", resp.StatusCode)
	}
	if resp, b := ta.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": "alice", "password": "password1"}); resp.StatusCode != 403 {
		t.Fatalf("停用后不能登录: %d %s", resp.StatusCode, b)
	}
	console("POST", "/api/v1/console/users/"+id+"/disable", map[string]any{"disabled": false})
	if resp, b := console("POST", "/api/v1/console/users/"+id+"/password", map[string]any{"password": "newpassword"}); resp.StatusCode != 204 {
		t.Fatalf("reset password: %d %s", resp.StatusCode, b)
	}
	login("alice", "newpassword")
	if resp, _ := console("POST", "/api/v1/console/users/"+id+"/delete", map[string]any{"confirm": "bob"}); resp.StatusCode != 400 {
		t.Fatalf("用户名不对不应删除: %d", resp.StatusCode)
	}
	if resp, b := console("POST", "/api/v1/console/users/"+id+"/delete", map[string]any{"confirm": "alice"}); resp.StatusCode != 204 {
		t.Fatalf("delete: %d %s", resp.StatusCode, b)
	}
	if _, err := ta.DB.Users().GetByID(ctx, alice.ID); err == nil {
		t.Fatal("用户应已删除")
	}
}

// 管理员密码还是默认的 1228 时，会员和用户管理都不能用。
func TestBillingNeedsStrongAdminPassword(t *testing.T) {
	ta := newTestApp(t, multiConfig)
	_, b := ta.do(t, "POST", "/api/v1/console/login", "", map[string]string{"password": "1228"})
	var cl struct{ Token string }
	_ = json.Unmarshal(b, &cl)
	req, _ := http.NewRequest("GET", ta.srv.URL+"/api/v1/console/users", nil)
	req.Header.Set("X-Console-Token", cl.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("默认密码时应拒绝: %d", resp.StatusCode)
	}
}
