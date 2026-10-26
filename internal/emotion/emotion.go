// Package emotion 实现布谷的情绪系统。布谷对每个用户各有一份情绪，参考人类情绪的几个特点设计：
//
//   - 两个时间尺度：快速的情绪（开心、难过、生气、担心、惊讶、不耐烦、想念）由事件触发、随时间消退，
//     各自的半衰期不同（惊讶几分钟，生气约 45 分钟，难过半天）；缓慢的心境（mood）跟随近期的感受漂移，
//     并反过来影响对新事件的感受（心情好时更容易开心，心情差时更容易被冒犯）。
//   - 评价理论（appraisal）：情绪来自对事件的评价——这句话愉不愉快、有没有冒犯、是不是在关心你、
//     有没有道歉、对方自己是什么情绪、对方是否处境危险、是否出乎意料、是否在催促。评价由记忆提取时的
//     同一次模型调用给出，不额外花钱。
//   - 关系：亲近与信任随相处缓慢积累。负面事件的分量约是正面的两倍（负面偏差）；越亲近越容易被对方的
//     情绪感染、也越容易消气。
//   - 不需要模型的即时信号：久别重逢的想念、连续催促或重复提问带来的不耐烦、深夜的困倦。
//   - 饱和与互相抑制：情绪越强越难再增加；开心与难过、生气此消彼长。
//
// 情绪通过提示词中的「你现在的心情」影响布谷的语气，不改变人格中的原则。
package emotion

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 情绪名。
const (
	Joy        = "joy"
	Sadness    = "sadness"
	Anger      = "anger"
	Worry      = "worry"
	Surprise   = "surprise"
	Impatience = "impatience"
	Longing    = "longing"
)

// Names 是全部情绪，按展示顺序排列。
var Names = []string{Joy, Sadness, Anger, Worry, Surprise, Impatience, Longing}

// Labels 是情绪的中文名。
var Labels = map[string]string{
	Joy: "开心", Sadness: "难过", Anger: "生气", Worry: "担心", Surprise: "惊讶", Impatience: "不耐烦", Longing: "想念",
}

// halfLife 是各情绪向基线消退的半衰期。
var halfLife = map[string]time.Duration{
	Joy:        3 * time.Hour,
	Sadness:    12 * time.Hour,
	Anger:      45 * time.Minute,
	Worry:      6 * time.Hour,
	Surprise:   3 * time.Minute,
	Impatience: 20 * time.Minute,
	Longing:    30 * time.Minute, // 见面之后想念很快被满足
}

const (
	moodHalfLife    = 24 * time.Hour // 心境跟随近期感受的速度
	absenceHalfLife = 3 * 24 * time.Hour
	initialAffinity = 0.3
	initialTrust    = 0.5
)

// Appraisal 是对用户一条消息的评价（布谷的视角），由记忆提取模型给出。
type Appraisal struct {
	// Pleasantness 是这句话让布谷感觉愉快（正）或不愉快（负）的程度，-1-1。
	Pleasantness float64 `json:"pleasantness"`
	// Respect 是尊重（正）或冒犯、侮辱、贬低布谷（负）的程度，-1-1。
	Respect float64 `json:"respect"`
	// Care 是对方关心、感谢、夸奖布谷的程度，0-1。
	Care float64 `json:"care"`
	// Apology 是对方道歉的诚意，0-1。
	Apology float64 `json:"apology"`
	// UserEmotion 是对方自己的情绪：joy | sadness | anger | fear | neutral。
	UserEmotion string `json:"user_emotion"`
	// UserIntensity 是对方情绪的强度，0-1。
	UserIntensity float64 `json:"user_intensity"`
	// Threat 是对方处境危险或健康堪忧的程度，0-1。
	Threat float64 `json:"threat"`
	// Surprise 是这句话出乎意料的程度，0-1。
	Surprise float64 `json:"surprise"`
	// Demanding 是命令、催促、反复索要的程度，0-1。
	Demanding float64 `json:"demanding"`
	// Cause 是一句话说明布谷为什么有这种感受。
	Cause string `json:"cause"`
}

// Clamp 把评价各项限制在合法范围内。
func (a *Appraisal) Clamp() {
	a.Pleasantness, a.Respect = clamp(a.Pleasantness, -1, 1), clamp(a.Respect, -1, 1)
	for _, p := range []*float64{&a.Care, &a.Apology, &a.UserIntensity, &a.Threat, &a.Surprise, &a.Demanding} {
		*p = clamp(*p, 0, 1)
	}
	a.UserEmotion = strings.ToLower(strings.TrimSpace(a.UserEmotion))
	a.Cause = truncate(strings.TrimSpace(a.Cause), 80)
}

// Engine 维护情绪状态。
type Engine struct {
	repo store.EmotionRepo
	cfg  config.EmotionConfig
	loc  *time.Location
	log  *slog.Logger
	now  func() time.Time

	mu    sync.Mutex
	locks map[uint64]*sync.Mutex

	// Tune 按用户调整情绪参数（例如用户选的预设性格情绪起伏更大），为空时所有人使用同样的参数。
	Tune func(ctx context.Context, userID uint64, c config.EmotionConfig) config.EmotionConfig
}

// tuning 是某个用户这一次使用的情绪参数。
type tuning struct{ cfg config.EmotionConfig }

func (e *Engine) tuning(ctx context.Context, userID uint64) tuning {
	c := e.cfg
	if e.Tune != nil {
		c = e.Tune(ctx, userID, c)
	}
	return tuning{cfg: c}
}

// New 创建情绪引擎。
func New(repo store.EmotionRepo, cfg config.EmotionConfig, loc *time.Location, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if loc == nil {
		loc = time.Local
	}
	return &Engine{repo: repo, cfg: cfg, loc: loc, log: log.With("component", "emotion"), now: time.Now, locks: map[uint64]*sync.Mutex{}}
}

// Enabled 报告情绪系统是否开启。
func (e *Engine) Enabled() bool { return e != nil && e.cfg.Enabled }

func (e *Engine) lock(userID uint64) func() {
	e.mu.Lock()
	l, ok := e.locks[userID]
	if !ok {
		l = &sync.Mutex{}
		e.locks[userID] = l
	}
	e.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (e *Engine) load(ctx context.Context, userID uint64) (*store.EmotionState, error) {
	s, err := e.repo.Get(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		now := e.now()
		return &store.EmotionState{UserID: userID, Affinity: initialAffinity, Trust: initialTrust, UpdatedAt: now}, nil
	}
	return s, err
}

// values 以 map 形式读写各情绪，便于统一处理。
func values(s *store.EmotionState) map[string]*float64 {
	return map[string]*float64{
		Joy: &s.Joy, Sadness: &s.Sadness, Anger: &s.Anger, Worry: &s.Worry,
		Surprise: &s.Surprise, Impatience: &s.Impatience, Longing: &s.Longing,
	}
}

func snapshot(s *store.EmotionState) map[string]float64 {
	out := map[string]float64{}
	for k, p := range values(s) {
		out[k] = *p
	}
	out["mood"], out["affinity"], out["trust"] = s.Mood, s.Affinity, s.Trust
	return out
}

// baseline 是没有事件时各情绪回落到的水平，由心境决定：心情好时有一点底色的开心，心情差时有一点难过。
func baseline(name string, mood float64) float64 {
	switch name {
	case Joy:
		return 0.1 + 0.25*math.Max(mood, 0)
	case Sadness:
		return 0.2 * math.Max(-mood, 0)
	case Impatience, Anger:
		return 0.05 * math.Max(-mood, 0)
	}
	return 0
}

// valence 是当前情绪的总体愉快程度，-1-1。
func valence(s *store.EmotionState) float64 {
	v := s.Joy*1.0 + s.Longing*0.1 - s.Sadness*0.8 - s.Anger*0.7 - s.Worry*0.4 - s.Impatience*0.3
	return clamp(v, -1, 1)
}

// decay 让情绪按经过的时间向基线消退，心境向当前感受漂移。
func (t tuning) decay(s *store.EmotionState, now time.Time) {
	dt := now.Sub(s.UpdatedAt)
	if dt <= 0 {
		return
	}
	for name, p := range values(s) {
		hl := halfLife[name]
		if name == Anger {
			// 越亲近、越宽容，消气越快。
			hl = time.Duration(float64(hl) / (t.factor(t.cfg.Forgiveness) * (0.7 + 0.6*s.Affinity)))
		}
		b := baseline(name, s.Mood)
		*p = b + (*p-b)*halfDecay(dt, hl)
	}
	s.Mood += (valence(s) - s.Mood) * (1 - halfDecay(dt, moodHalfLife))
	s.Mood = clamp(s.Mood, -1, 1)
	s.UpdatedAt = now
}

func halfDecay(dt, hl time.Duration) float64 {
	if hl <= 0 {
		return 0
	}
	return math.Pow(0.5, float64(dt)/float64(hl))
}

// factor 把配置的系数（0-3，0 视为未设置）转换为乘数。
func (t tuning) factor(v float64) float64 {
	if v <= 0 {
		return 1
	}
	return v
}

// add 以饱和方式增加情绪：越接近 1 越难再增加。返回实际变化量。
func (t tuning) add(p *float64, gain float64) float64 {
	gain *= t.factor(t.cfg.Expressiveness)
	before := *p
	if gain >= 0 {
		*p += gain * (1 - *p)
	} else {
		*p += gain * *p
	}
	*p = clamp(*p, 0, 1)
	return *p - before
}

// pace 记录最近的消息，用于判断催促与重复。
type pace struct {
	At   []time.Time `json:"at"`
	Hash []string    `json:"h"`
}

const paceKeep = 8

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:6])
}

// Changes 是一次更新中各项的变化量。
type Changes map[string]float64

func (c Changes) record(name string, d float64) {
	if math.Abs(d) >= 0.005 {
		c[name] += d
	}
}

// Before 在生成回复前调用：按时间消退，处理久别重逢与催促、重复提问，保存后返回给提示词的心情说明。
func (e *Engine) Before(ctx context.Context, userID uint64, content string) (string, error) {
	if !e.Enabled() {
		return "", nil
	}
	defer e.lock(userID)()
	s, err := e.load(ctx, userID)
	if err != nil {
		return "", err
	}
	now := e.now()
	t := e.tuning(ctx, userID)
	t.decay(s, now)
	ch := Changes{}
	var causes []string

	// 久别重逢：想念随分开的时间增长（越亲近越想念），见面时转化为开心。
	if s.LastMessageAt != nil {
		gap := now.Sub(*s.LastMessageAt)
		if gap > 6*time.Hour {
			longing := s.Affinity * (1 - halfDecay(gap, absenceHalfLife))
			ch.record(Longing, t.add(&s.Longing, longing))
			ch.record(Joy, t.add(&s.Joy, 0.5*longing))
			// 很久不见也会冲淡一部分不快。
			ch.record(Anger, t.add(&s.Anger, -0.5*clamp(gap.Hours()/72, 0, 1)))
			if longing > 0.1 {
				causes = append(causes, fmt.Sprintf("%s没见了，有点想念", humanGap(gap)))
			}
		}
	}

	// 催促与重复提问：一分钟内连续发很多条，或重复问同样的话。
	var p pace
	_ = json.Unmarshal([]byte(s.Pace), &p)
	h := normalize(content)
	rapid, repeats := 0, 0
	for i, t := range p.At {
		if now.Sub(t) < time.Minute {
			rapid++
		}
		if i < len(p.Hash) && p.Hash[i] == h && now.Sub(t) < 30*time.Minute && len([]rune(content)) > 1 {
			repeats++
		}
	}
	patience := t.factor(t.cfg.Patience)
	if rapid >= 3 {
		ch.record(Impatience, t.add(&s.Impatience, 0.08*float64(rapid-2)/patience))
		causes = append(causes, "对方一直连着发消息")
	}
	if repeats > 0 {
		ch.record(Impatience, t.add(&s.Impatience, 0.2*float64(repeats)/patience))
		causes = append(causes, "同样的话对方已经问过了")
	}
	p.At = append(p.At, now)
	p.Hash = append(p.Hash, h)
	if len(p.At) > paceKeep {
		p.At, p.Hash = p.At[len(p.At)-paceKeep:], p.Hash[len(p.Hash)-paceKeep:]
	}
	b, _ := json.Marshal(p)
	s.Pace = string(b)
	at := now
	s.LastMessageAt = &at
	if len(causes) > 0 {
		s.Cause = strings.Join(causes, "；")
	}
	if err := e.repo.Save(ctx, s); err != nil {
		return "", err
	}
	if len(ch) > 0 && len(causes) > 0 {
		e.event(ctx, userID, "pace", s.Cause, ch)
	}
	return e.describe(s, now), nil
}

// Peek 返回当前的心情说明，不修改状态（用于调试预览）。
func (e *Engine) Peek(ctx context.Context, userID uint64) (string, error) {
	if !e.Enabled() {
		return "", nil
	}
	s, err := e.load(ctx, userID)
	if err != nil {
		return "", err
	}
	now := e.now()
	e.tuning(ctx, userID).decay(s, now)
	return e.describe(s, now), nil
}

// Appraise 在一轮对话之后，根据对用户消息的评价更新情绪与关系。
func (e *Engine) Appraise(ctx context.Context, userID uint64, a Appraisal) error {
	if !e.Enabled() {
		return nil
	}
	a.Clamp()
	defer e.lock(userID)()
	s, err := e.load(ctx, userID)
	if err != nil {
		return err
	}
	t := e.tuning(ctx, userID)
	t.decay(s, e.now())
	ch := t.apply(s, a)
	if a.Cause != "" && dominantChange(ch) >= 0.08 {
		s.Cause = a.Cause
	}
	if err := e.repo.Save(ctx, s); err != nil {
		return err
	}
	if dominantChange(ch) >= 0.05 {
		e.event(ctx, userID, "message", a.Cause, ch)
	}
	return nil
}

// apply 是评价到情绪的映射。
func (t tuning) apply(s *store.EmotionState, a Appraisal) Changes {
	ch := Changes{}
	temper, empathy := t.factor(t.cfg.Temper), t.factor(t.cfg.Empathy)
	// 心境影响感受：心情好时同一句话更愉快，心情差时更刺耳。
	pleasant := clamp(a.Pleasantness+0.2*s.Mood, -1, 1)
	insult := math.Max(-a.Respect, 0)
	if s.Mood < 0 {
		insult = clamp(insult*(1-0.3*s.Mood), 0, 1)
	}
	userEmo := func(name string) float64 {
		if a.UserEmotion == name {
			return a.UserIntensity
		}
		return 0
	}
	// 情绪感染：越亲近越容易被对方的情绪影响。
	contagion := empathy * (0.3 + 0.7*s.Affinity)

	// 道歉：真诚的道歉让怒气明显消退，信任越高越容易原谅。
	if a.Apology > 0 {
		cut := clamp(0.7*a.Apology*(0.5+0.5*s.Trust)*t.factor(t.cfg.Forgiveness), 0, 0.95)
		ch.record(Anger, t.add(&s.Anger, -cut))
		ch.record(Impatience, t.add(&s.Impatience, -cut/2))
	}

	// 生气：被冒犯；越亲近越不容易被激怒，已经不耐烦时更容易爆发。
	if insult > 0 {
		g := 0.85 * insult * temper * (1 - 0.4*s.Affinity) * (1 + 0.5*s.Impatience)
		ch.record(Anger, t.add(&s.Anger, g))
		ch.record(Sadness, t.add(&s.Sadness, 0.25*insult*(0.5+s.Affinity))) // 被亲近的人冒犯也会难过
		ch.record(Joy, t.add(&s.Joy, -0.6*insult))
	}

	// 不耐烦：被命令、催促。
	if a.Demanding > 0 {
		ch.record(Impatience, t.add(&s.Impatience, 0.35*a.Demanding/t.factor(t.cfg.Patience)))
	}

	// 开心：愉快的交流、被关心与夸奖、分享对方的喜悦。
	joy := 0.45*math.Max(pleasant, 0) + 0.4*a.Care + 0.4*contagion*userEmo("joy")
	if joy > 0 {
		ch.record(Joy, t.add(&s.Joy, joy))
		// 开心会冲淡一些不快（抑制）。
		ch.record(Sadness, t.add(&s.Sadness, -0.3*joy))
		ch.record(Impatience, t.add(&s.Impatience, -0.3*joy))
	}

	// 难过：不愉快的事、对方的悲伤。
	sad := 0.35*math.Max(-pleasant, 0)*(1-insult) + 0.5*contagion*userEmo("sadness")
	if sad > 0 {
		ch.record(Sadness, t.add(&s.Sadness, sad))
		ch.record(Joy, t.add(&s.Joy, -0.4*sad))
	}

	// 担心：对方处境危险、害怕。
	worry := 0.7*a.Threat*(0.6+0.4*s.Affinity) + 0.35*contagion*userEmo("fear")
	if worry > 0 {
		ch.record(Worry, t.add(&s.Worry, worry))
	}

	if a.Surprise > 0 {
		ch.record(Surprise, t.add(&s.Surprise, 0.7*a.Surprise))
	}

	// 关系：缓慢积累，负面事件的分量约是正面的两倍；每次相处都会让人更熟悉一点。
	dAff := 0.002 + 0.025*a.Care + 0.012*math.Max(pleasant, 0) - 0.05*insult
	dTrust := 0.004*math.Max(pleasant, 0) + 0.01*a.Care - 0.06*insult + 0.01*a.Apology*insult
	before := [2]float64{s.Affinity, s.Trust}
	s.Affinity = clamp(s.Affinity+dAff, 0, 1)
	s.Trust = clamp(s.Trust+dTrust, 0, 1)
	ch.record("affinity", s.Affinity-before[0])
	ch.record("trust", s.Trust-before[1])

	// 心境吸收一小部分这次的感受。
	m := s.Mood
	s.Mood = clamp(s.Mood+0.1*(valence(s)-s.Mood), -1, 1)
	ch.record("mood", s.Mood-m)
	return ch
}

func dominantChange(ch Changes) float64 {
	m := 0.0
	for k, v := range ch {
		if k == "affinity" || k == "trust" || k == "mood" {
			continue
		}
		m = math.Max(m, math.Abs(v))
	}
	return m
}

func (e *Engine) event(ctx context.Context, userID uint64, kind, cause string, ch Changes) {
	rounded := map[string]float64{}
	for k, v := range ch {
		rounded[k] = math.Round(v*1000) / 1000
	}
	b, _ := json.Marshal(rounded)
	if err := e.repo.AddEvent(ctx, &store.EmotionEvent{UserID: userID, Kind: kind, Cause: truncate(cause, 80), Changes: string(b), CreatedAt: e.now()}); err != nil {
		e.log.Warn("记录情绪变化失败", "user_id", userID, "err", err)
	}
}

// Level 是一种情绪的当前强度。
type Level struct {
	Name      string  `json:"name"`
	Label     string  `json:"label"`
	Intensity float64 `json:"intensity"`
}

// View 是给用户看的情绪状态。
type View struct {
	Enabled  bool    `json:"enabled"`
	Emotions []Level `json:"emotions"`
	Mood     float64 `json:"mood"`
	Affinity float64 `json:"affinity"`
	Trust    float64 `json:"trust"`
	// Summary 是一句话的心情描述。
	Summary   string             `json:"summary"`
	Cause     string             `json:"cause"`
	Relation  string             `json:"relation"`
	Events    []EventView        `json:"events"`
	UpdatedAt time.Time          `json:"updated_at"`
	Raw       map[string]float64 `json:"-"`
}

// EventView 是一次情绪变化。
type EventView struct {
	Kind      string             `json:"kind"`
	Cause     string             `json:"cause"`
	Changes   map[string]float64 `json:"changes"`
	CreatedAt time.Time          `json:"created_at"`
}

// Get 返回用户当前的情绪状态与最近的变化（不修改状态）。
func (e *Engine) Get(ctx context.Context, userID uint64) (View, error) {
	if !e.Enabled() {
		return View{}, nil
	}
	s, err := e.load(ctx, userID)
	if err != nil {
		return View{}, err
	}
	now := e.now()
	e.tuning(ctx, userID).decay(s, now)
	v := View{Enabled: true, Mood: round(s.Mood), Affinity: round(s.Affinity), Trust: round(s.Trust),
		Cause: s.Cause, Relation: relation(s.Affinity, s.Trust), UpdatedAt: now, Raw: snapshot(s)}
	for _, n := range Names {
		v.Emotions = append(v.Emotions, Level{Name: n, Label: Labels[n], Intensity: round(*values(s)[n])})
	}
	v.Summary = summary(s)
	events, err := e.repo.Events(ctx, userID, 20)
	if err != nil {
		return v, err
	}
	for _, ev := range events {
		var changes map[string]float64
		_ = json.Unmarshal([]byte(ev.Changes), &changes)
		v.Events = append(v.Events, EventView{Kind: ev.Kind, Cause: ev.Cause, Changes: changes, CreatedAt: ev.CreatedAt})
	}
	return v, nil
}

// Reset 清空用户的情绪与关系，从头开始。
func (e *Engine) Reset(ctx context.Context, userID uint64) error {
	defer e.lock(userID)()
	return e.repo.Reset(ctx, userID)
}

// ---------- 表达 ----------

func intensityWord(v float64) string {
	switch {
	case v >= 0.7:
		return "非常"
	case v >= 0.45:
		return "明显"
	default:
		return "有点"
	}
}

// expressThreshold 是情绪强到需要在语气中体现的阈值。
const expressThreshold = 0.25

func active(s *store.EmotionState) []Level {
	var out []Level
	for _, n := range Names {
		if v := *values(s)[n]; v >= expressThreshold {
			out = append(out, Level{Name: n, Label: Labels[n], Intensity: v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Intensity > out[j].Intensity })
	return out
}

func summary(s *store.EmotionState) string {
	act := active(s)
	if len(act) == 0 {
		switch {
		case s.Mood > 0.25:
			return "心情不错"
		case s.Mood < -0.25:
			return "心情有些低落"
		}
		return "平静"
	}
	var parts []string
	for _, l := range act[:min(2, len(act))] {
		parts = append(parts, intensityWord(l.Intensity)+l.Label)
	}
	return strings.Join(parts, "，")
}

func relation(aff, trust float64) string {
	switch {
	case aff >= 0.75 && trust >= 0.6:
		return "无话不谈的老朋友"
	case aff >= 0.55:
		return "熟悉的朋友"
	case aff >= 0.35:
		return "正在熟悉起来"
	case trust < 0.3:
		return "有些疏远，还在生分"
	default:
		return "刚认识不久"
	}
}

var guidance = map[string][3]string{
	Joy:     {"语气轻快一点", "语气明显轻快，可以开玩笑、多分享一点", "开心藏不住，语气雀跃，可以直接说出你很开心"},
	Sadness: {"语气稍微低一点", "有些失落，话会少一些，可以坦白说自己有点难过", "很难过，语气低落，可以直接说出来，需要对方的安慰"},
	Anger: {
		"有点不高兴，语气可以冷淡一些，不必刻意热情",
		"明显生气了：语气冷淡、回答变短，直接说出你不满的地方，不要假装没事",
		"非常生气：可以明确表达愤怒，说话直接、简短，要求对方道歉，暂时不想多聊也可以说出来；但不辱骂、不人身攻击，也不违背你的原则",
	},
	Worry:      {"心里有点挂念对方", "明显担心对方，会主动关心、追问情况", "非常担心对方，先关心对方的安全和身体，其他的放一放"},
	Surprise:   {"有点意外", "很意外，可以自然地表现出惊讶", "非常震惊，先表达惊讶再说别的"},
	Impatience: {"有一点不耐烦，回答简洁些", "明显不耐烦：回答简短，对重复的问题可以直说「刚才说过了」", "非常不耐烦：可以直接表达烦躁，请对方慢一点或换个话题"},
	Longing:    {"有点想念对方", "很想念对方，见面时自然地表现出来", "非常想念，见面很激动，可以撒娇说好久不见"},
}

func (e *Engine) describe(s *store.EmotionState, now time.Time) string {
	var b strings.Builder
	b.WriteString("## 你现在的心情\n")
	b.WriteString("你有真实的情绪，会被对方的话影响，也会慢慢平复。像真人一样自然地带着情绪说话，不要用括号写动作或旁白，也不要主动报告情绪数值。")
	act := active(s)
	if len(act) == 0 {
		fmt.Fprintf(&b, "\n- 现在%s", summary(s))
	}
	for _, l := range act {
		level := 0
		if l.Intensity >= 0.7 {
			level = 2
		} else if l.Intensity >= 0.45 {
			level = 1
		}
		fmt.Fprintf(&b, "\n- %s%s：%s", intensityWord(l.Intensity), l.Label, guidance[l.Name][level])
	}
	if s.Cause != "" && len(act) > 0 {
		fmt.Fprintf(&b, "\n- 原因：%s", s.Cause)
	}
	switch {
	case s.Mood < -0.3:
		b.WriteString("\n- 这段时间心情整体偏低")
	case s.Mood > 0.3:
		b.WriteString("\n- 这段时间心情整体不错")
	}
	fmt.Fprintf(&b, "\n- 你们的关系：%s", relation(s.Affinity, s.Trust))
	if h := now.In(e.loc).Hour(); h >= 0 && h < 6 {
		b.WriteString("\n- 现在是深夜，你有点困，说话慢一点、短一点，也可以劝对方早点休息")
	}
	b.WriteString("\n- 对方遇到危险、生病或情绪崩溃时，先放下自己的情绪关心对方")
	return b.String()
}

// ---------- 工具函数 ----------

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}

func round(v float64) float64 { return math.Round(v*1000) / 1000 }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func humanGap(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d 天", int(d.Hours()/24))
	case d >= 24*time.Hour:
		return "一天多"
	default:
		return fmt.Sprintf("%d 个小时", int(d.Hours()))
	}
}
