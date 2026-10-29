package opencode

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 解析官网文档 https://opencode.ai/docs/go/ 的表格：
//   - 套餐表（Plan / Price）：各档名字和月费；
//   - 每档一张价格表（Model / Input / Output / Cached Read / Cached Write / Monthly limit），
//     放在 data-plan-panel="go" / "go-plus" / "go-teams" 的标签页里；
//   - 接口表（Model / Model ID / Endpoint）：显示名对应的模型名。
//
// 官网改版导致解析不出来时返回错误，继续用之前的目录。

var (
	tableRe = regexp.MustCompile(`(?s)<table.*?</table>`)
	rowRe   = regexp.MustCompile(`(?s)<tr.*?</tr>`)
	cellRe  = regexp.MustCompile(`(?s)<t[hd][^>]*>(.*?)</t[hd]>`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	panelRe = regexp.MustCompile(`data-plan-panel="([a-z0-9-]+)"`)
	moneyRe = regexp.MustCompile(`\$\s*([0-9]+(?:\.[0-9]+)?)`)
	parenRe = regexp.MustCompile(`\s*\(([^)]*)\)\s*$`)
)

type table struct {
	pos  int
	head []string
	rows [][]string
}

func cellText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, " "))), " ")
}

func tables(page string) []table {
	var out []table
	for _, loc := range tableRe.FindAllStringIndex(page, -1) {
		t := table{pos: loc[0]}
		for i, r := range rowRe.FindAllString(page[loc[0]:loc[1]], -1) {
			var cells []string
			for _, c := range cellRe.FindAllStringSubmatch(r, -1) {
				cells = append(cells, cellText(c[1]))
			}
			if i == 0 {
				t.head = cells
			} else if len(cells) > 0 {
				t.rows = append(t.rows, cells)
			}
		}
		out = append(out, t)
	}
	return out
}

func hasHead(t table, names ...string) bool {
	h := strings.ToLower(strings.Join(t.head, "|"))
	for _, n := range names {
		if !strings.Contains(h, strings.ToLower(n)) {
			return false
		}
	}
	return true
}

// money 取一格里的美元金额；有原价和优惠价（「$4.00 $2.00」）时取最后一个（优惠价）。
func money(s string) (float64, bool) {
	ms := moneyRe.FindAllStringSubmatch(s, -1)
	if len(ms) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(ms[len(ms)-1][1], 64)
	return v, err == nil
}

func tierKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

// priceRow 是价格表里的一行。
type priceRow struct {
	name, variant string
	rate          Rate
	limit         float64
	free          bool
}

func parsePriceRow(cells []string) (priceRow, error) {
	if len(cells) < 6 {
		return priceRow{}, fmt.Errorf("价格表的列数不对：%v", cells)
	}
	r := priceRow{name: cells[0]}
	if m := parenRe.FindStringSubmatch(r.name); m != nil {
		r.variant = strings.ToLower(m[1])
		r.name = strings.TrimSpace(r.name[:len(r.name)-len(m[0])])
	}
	if strings.EqualFold(cells[1], "free") {
		r.free = true
		return r, nil
	}
	var ok1, ok2, ok3 bool
	r.rate.Input, ok1 = money(cells[1])
	r.rate.Output, ok2 = money(cells[2])
	r.rate.Cached, ok3 = money(cells[3])
	if !ok1 || !ok2 {
		return priceRow{}, fmt.Errorf("「%s」的价格读不出来", cells[0])
	}
	if !ok3 {
		r.rate.Cached = r.rate.Input
	}
	limit, ok := money(cells[len(cells)-1])
	if !ok {
		if strings.Contains(strings.ToLower(cells[len(cells)-1]), "unlimited") {
			r.free = true
			return r, nil
		}
		return priceRow{}, fmt.Errorf("「%s」的月额度读不出来", cells[0])
	}
	r.limit = limit
	return r, nil
}

// Parse 从官网文档的 HTML 解析出目录。
func Parse(page string, now time.Time) (*Snapshot, error) {
	ts := tables(page)
	snap := &Snapshot{Date: now.Format("2006-01-02"), Source: "synced", Plans: map[string]Plan{}}
	fetched := now
	snap.FetchedAt = &fetched

	// 套餐表。
	for _, t := range ts {
		if !hasHead(t, "plan", "price") {
			continue
		}
		for _, r := range t.rows {
			if len(r) < 2 {
				continue
			}
			price, ok := money(r[1])
			if !ok {
				continue
			}
			key := tierKey(r[0])
			snap.Plans[key] = Plan{Name: r[0], Price: price, PerMember: strings.Contains(strings.ToLower(r[1]), "member")}
			snap.Tiers = append(snap.Tiers, key)
		}
		break
	}
	if len(snap.Plans) == 0 {
		return nil, errors.New("没找到套餐表")
	}

	// 接口表：显示名 → 模型名。
	ids := map[string]string{}
	for _, t := range ts {
		if !hasHead(t, "model id") {
			continue
		}
		for _, r := range t.rows {
			if len(r) >= 2 && r[1] != "" {
				ids[strings.ToLower(r[0])] = r[1]
			}
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("没找到模型名的表")
	}

	// 各档的价格表：价格各档相同，从第一张表取；额度每档一张。
	panels := panelRe.FindAllStringSubmatchIndex(page, -1)
	panelAt := func(pos int) string {
		key := ""
		for _, p := range panels {
			if p[0] > pos {
				break
			}
			key = page[p[2]:p[3]]
		}
		return key
	}
	byID := map[string]*Model{}
	var order []string
	seen := map[string]bool{} // tier|id：分段计价的模型只取第一行（较低一档）
	for _, t := range ts {
		if !hasHead(t, "model", "input", "output", "monthly limit") {
			continue
		}
		tier := panelAt(t.pos)
		if _, ok := snap.Plans[tier]; !ok {
			continue
		}
		for _, cells := range t.rows {
			r, err := parsePriceRow(cells)
			if err != nil {
				return nil, err
			}
			id, ok := ids[strings.ToLower(r.name)]
			if !ok {
				continue // 没有接口的模型调用不了
			}
			m := byID[id]
			if m == nil {
				m = &Model{ID: id, Name: r.name, Limits: map[string]float64{}, Free: r.free}
				byID[id] = m
				order = append(order, id)
			}
			v := r.variant
			switch {
			case strings.Contains(v, "off-peak") || strings.Contains(v, "off peak"):
				if m.OffPeak == nil {
					rate := r.rate
					m.OffPeak = &rate
				}
			case strings.Contains(v, "peak"):
				m.Rate = r.rate // 高峰价作为主价格，空闲价放在 OffPeak
			default:
				// 分段计价（≤ 256K / > 256K）只取第一行，也就是较低一档。
				if seen[tier+"|"+id] {
					continue
				}
				seen[tier+"|"+id] = true
				if strings.Contains(v, "token") {
					m.Note = tieredNote
				} else if strings.Contains(v, "off") {
					m.Note = "按优惠价计（官方写明是限时优惠）"
				}
				if m.Rate == (Rate{}) {
					m.Rate = r.rate
				}
			}
			m.Limits[tier] = r.limit
		}
	}
	for _, id := range order {
		m := byID[id]
		// 把内置目录里人工核对过的信息（能不能看图、注意事项）带上。
		if b, ok := Builtin.Find(id); ok {
			m.Vision = b.Vision
			if b.Note != "" {
				m.Note = b.Note
			}
		}
		snap.Models = append(snap.Models, *m)
	}
	if err := snap.validate(); err != nil {
		return nil, err
	}
	return snap, nil
}

// validate 做一遍检查，防止官网改版读错了价格（计费跟钱有关，宁可不更新）。
func (s *Snapshot) validate() error {
	if len(s.Models) < 5 {
		return fmt.Errorf("只读到 %d 个模型，可能是官网改版了", len(s.Models))
	}
	for _, m := range s.Models {
		if m.Free {
			continue
		}
		for _, x := range []float64{m.Rate.Input, m.Rate.Output, m.Rate.Cached} {
			if x < 0 || x > 1000 {
				return fmt.Errorf("「%s」的价格不对劲（$%v）", m.Name, x)
			}
		}
		if m.Rate.Input == 0 && m.Rate.Output == 0 {
			return fmt.Errorf("「%s」没有价格", m.Name)
		}
		for _, tier := range s.Tiers {
			if l := m.Limits[tier]; l <= 0 || l > 100000 {
				return fmt.Errorf("「%s」在 %s 档的月额度不对劲（$%v）", m.Name, s.Plans[tier].Name, l)
			}
		}
	}
	return nil
}
