package emotion

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newEngine(t *testing.T, mutate func(*config.EmotionConfig)) (*Engine, *clock) {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "e.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default().Emotion
	if mutate != nil {
		mutate(&cfg)
	}
	c := &clock{t: time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)}
	e := New(db.Emotions(), cfg, time.UTC, nil)
	e.now = c.now
	return e, c
}

func level(t *testing.T, e *Engine, user uint64, name string) float64 {
	t.Helper()
	v, err := e.Get(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	return v.Raw[name]
}

var insult = Appraisal{Pleasantness: -0.8, Respect: -0.9, UserEmotion: "anger", UserIntensity: 0.7, Cause: "被骂是笨蛋"}

func TestAngerRisesFadesAndForgives(t *testing.T) {
	ctx := context.Background()
	e, c := newEngine(t, nil)
	if _, err := e.Before(ctx, 1, "你就是个笨蛋"); err != nil {
		t.Fatal(err)
	}
	if err := e.Appraise(ctx, 1, insult); err != nil {
		t.Fatal(err)
	}
	anger := level(t, e, 1, Anger)
	if anger < 0.5 {
		t.Fatalf("被骂后应明显生气: %.3f", anger)
	}
	if level(t, e, 1, "trust") >= initialTrust || level(t, e, 1, "affinity") >= initialAffinity {
		t.Fatal("被冒犯后信任与亲近应下降")
	}
	desc, _ := e.Peek(ctx, 1)
	if !strings.Contains(desc, "生气") || !strings.Contains(desc, "被骂是笨蛋") {
		t.Fatalf("心情说明应包含生气与原因: %s", desc)
	}

	// 一个小时后怒气消退一半以上，但还没消完。
	c.advance(time.Hour)
	later := level(t, e, 1, Anger)
	if later > anger*0.6 || later < 0.05 {
		t.Fatalf("一小时后的怒气: %.3f（原 %.3f）", later, anger)
	}

	// 再被骂一次又升起来，然后真诚道歉明显消气。
	_ = e.Appraise(ctx, 1, insult)
	before := level(t, e, 1, Anger)
	_ = e.Appraise(ctx, 1, Appraisal{Pleasantness: 0.3, Respect: 0.8, Apology: 1, UserEmotion: "sadness", UserIntensity: 0.4, Cause: "对方道歉了"})
	after := level(t, e, 1, Anger)
	if after > before*0.65 {
		t.Fatalf("道歉后应明显消气: %.3f → %.3f", before, after)
	}

	v, _ := e.Get(ctx, 1)
	if len(v.Events) < 3 || v.Events[0].Cause != "对方道歉了" {
		t.Fatalf("应记录情绪变化: %+v", v.Events)
	}
}

func TestCloseFriendsForgiveFaster(t *testing.T) {
	ctx := context.Background()
	e, c := newEngine(t, nil)
	// 用户 2 与布谷相处融洽很久。
	for i := 0; i < 60; i++ {
		_ = e.Appraise(ctx, 2, Appraisal{Pleasantness: 0.7, Respect: 0.8, Care: 0.8, UserEmotion: "joy", UserIntensity: 0.6})
		c.advance(2 * time.Hour)
	}
	if a := level(t, e, 2, "affinity"); a < 0.6 {
		t.Fatalf("长期友好相处后应变亲近: %.3f", a)
	}
	if m := level(t, e, 2, "mood"); m <= 0.1 {
		t.Fatalf("心境应偏愉快: %.3f", m)
	}
	_ = e.Appraise(ctx, 1, insult)
	_ = e.Appraise(ctx, 2, insult)
	a1, a2 := level(t, e, 1, Anger), level(t, e, 2, Anger)
	if a2 >= a1 {
		t.Fatalf("亲近的人同样一句话引起的怒气应更小: 陌生 %.3f 亲近 %.3f", a1, a2)
	}
	c.advance(40 * time.Minute)
	if r1, r2 := level(t, e, 1, Anger)/a1, level(t, e, 2, Anger)/a2; r2 >= r1 {
		t.Fatalf("亲近的人应更快消气: 陌生剩 %.2f 亲近剩 %.2f", r1, r2)
	}
	// 生气只针对冒犯的人，不迁怒他人。
	if level(t, e, 3, Anger) != 0 {
		t.Fatal("情绪应按用户分开")
	}
}

func TestEmpathyWorryAndSurprise(t *testing.T) {
	ctx := context.Background()
	e, _ := newEngine(t, nil)
	_ = e.Appraise(ctx, 1, Appraisal{Pleasantness: -0.5, Respect: 0.5, UserEmotion: "sadness", UserIntensity: 0.9, Threat: 0.7, Surprise: 0.6})
	if level(t, e, 1, Sadness) < 0.25 || level(t, e, 1, Worry) < 0.3 || level(t, e, 1, Surprise) < 0.3 {
		t.Fatalf("对方难过且处境危险时应难过、担心: %+v", must(e.Get(ctx, 1)).Raw)
	}
	if level(t, e, 1, Anger) > 0.01 {
		t.Fatal("对方难过不应让布谷生气")
	}
}

func must(v View, _ error) View { return v }

func TestLongingAfterAbsence(t *testing.T) {
	ctx := context.Background()
	e, c := newEngine(t, nil)
	for i := 0; i < 30; i++ {
		_, _ = e.Before(ctx, 1, "聊天")
		_ = e.Appraise(ctx, 1, Appraisal{Pleasantness: 0.6, Respect: 0.6, Care: 0.6})
		c.advance(3 * time.Hour)
	}
	c.advance(5 * 24 * time.Hour)
	desc, err := e.Before(ctx, 1, "我回来啦")
	if err != nil {
		t.Fatal(err)
	}
	if level(t, e, 1, Longing) < 0.25 || !strings.Contains(desc, "想念") {
		t.Fatalf("久别重逢应想念: longing=%.3f\n%s", level(t, e, 1, Longing), desc)
	}
	// 见面聊一阵之后想念被满足。
	c.advance(2 * time.Hour)
	if level(t, e, 1, Longing) > 0.05 {
		t.Fatal("见面后想念应很快消退")
	}
}

func TestImpatienceFromRepeatsAndRapidMessages(t *testing.T) {
	ctx := context.Background()
	e, c := newEngine(t, nil)
	for i := 0; i < 3; i++ {
		_, _ = e.Before(ctx, 1, "你到底是谁？")
		c.advance(10 * time.Second)
	}
	imp := level(t, e, 1, Impatience)
	if imp < 0.3 {
		t.Fatalf("重复提问应不耐烦: %.3f", imp)
	}
	// 耐心更好的布谷没那么烦。
	e2, c2 := newEngine(t, func(c *config.EmotionConfig) { c.Patience = 3 })
	for i := 0; i < 3; i++ {
		_, _ = e2.Before(ctx, 1, "你到底是谁？")
		c2.advance(10 * time.Second)
	}
	if level(t, e2, 1, Impatience) >= imp {
		t.Fatal("耐心系数应降低不耐烦")
	}
	// 不同的话、间隔正常时不会不耐烦。
	e3, c3 := newEngine(t, nil)
	for _, m := range []string{"早", "今天下雨了", "你在干嘛"} {
		_, _ = e3.Before(ctx, 1, m)
		c3.advance(5 * time.Minute)
	}
	if level(t, e3, 1, Impatience) > 0.01 {
		t.Fatal("正常聊天不应不耐烦")
	}
}

func TestDescribeAndDisabled(t *testing.T) {
	ctx := context.Background()
	e, c := newEngine(t, nil)
	c.t = time.Date(2026, 10, 25, 2, 30, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_ = e.Appraise(ctx, 1, insult)
	}
	desc, _ := e.Before(ctx, 1, "怎么不说话")
	if !strings.Contains(desc, "非常生气") || !strings.Contains(desc, "深夜") || !strings.Contains(desc, "不辱骂") {
		t.Fatalf("desc=%s", desc)
	}

	off, _ := newEngine(t, func(c *config.EmotionConfig) { c.Enabled = false })
	if d, _ := off.Before(ctx, 1, "hi"); d != "" {
		t.Fatal("关闭后不应有心情说明")
	}
	if v, _ := off.Get(ctx, 1); v.Enabled {
		t.Fatal("关闭后 Enabled 应为 false")
	}

	if err := e.Reset(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if level(t, e, 1, Anger) != 0 || level(t, e, 1, "affinity") != initialAffinity {
		t.Fatal("重置后应回到初始状态")
	}
}
