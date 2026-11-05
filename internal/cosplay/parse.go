package cosplay

import (
	"encoding/json"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/HugehardJiang/buugoo/internal/prompt"
)

// Candidate 是解析出来、还没保存的一张卡（导入前给用户确认、修改）。
type Candidate struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Prompt      string   `json:"prompt"`
	Source      string   `json:"source,omitempty"`
	Runes       int      `json:"runes"`
	Tokens      int      `json:"tokens"`
	// TooLong 表示超过了长度上限，不能直接导入。
	TooLong bool `json:"too_long,omitempty"`
}

func (s *Service) finish(c Candidate) Candidate {
	c.Name = strings.Join(strings.Fields(c.Name), " ")
	if r := []rune(c.Name); len(r) > MaxNameRunes {
		c.Name = string(r[:MaxNameRunes])
	}
	c.Description = strings.Join(strings.Fields(c.Description), " ")
	if r := []rune(c.Description); len(r) > MaxDescRunes {
		c.Description = string(r[:MaxDescRunes-1]) + "…"
	}
	c.Prompt = normalizeText(c.Prompt)
	c.Runes = utf8.RuneCountInString(c.Prompt)
	c.Tokens = prompt.EstimateTokens(c.Prompt)
	c.TooLong = c.Runes > s.maxRunes
	return c
}

// ParseText 解析粘贴或上传的一张卡：支持纯文本 / Markdown 提示词、SillyTavern 角色卡 JSON，
// 以及从 aigengtu.com 复制下来的「===== FILE: … =====」格式。filename 用来猜名字，可以为空。
func (s *Service) ParseText(text, filename string) (Candidate, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	if strings.TrimSpace(text) == "" {
		return Candidate{}, &UserError{"内容是空的"}
	}
	if c, ok := parseCardJSON([]byte(text)); ok {
		return s.finish(c), nil
	}
	c := fromFiles(text)
	if c.Name == "" && filename != "" {
		base := path.Base(strings.ReplaceAll(filename, "\\", "/"))
		if name := strings.TrimSuffix(base, path.Ext(base)); name != "prompt" && name != "persona" {
			c.Name = name
		}
	}
	return s.finish(c), nil
}

var fileHeader = regexp.MustCompile(`(?m)^=====\s*FILE:\s*(.+?)\s*=====\s*$`)

// fromFiles 处理「===== FILE: 名字 =====」分隔的多个文件（aigengtu.com 的格式）：
// 取提示词文件作为正文，persona.json 里的标题和简介作为名字和说明。没有分隔时整段就是提示词。
func fromFiles(text string) Candidate {
	locs := fileHeader.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return Candidate{Name: guessName(text), Prompt: text}
	}
	var c Candidate
	var others []string
	for i, loc := range locs {
		name := text[loc[2]:loc[3]]
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := strings.TrimSpace(text[loc[1]:end])
		switch ext := strings.ToLower(path.Ext(name)); {
		case ext == ".json":
			var meta struct {
				Title       string `json:"title"`
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if json.Unmarshal([]byte(body), &meta) == nil {
				c.Name = firstNonEmpty(meta.Title, meta.Name)
				c.Description = meta.Description
			}
		case strings.HasSuffix(strings.ToLower(name), "prompt.txt") || strings.HasSuffix(strings.ToLower(name), "prompt.md"):
			c.Prompt = body
		default:
			others = append(others, body)
		}
	}
	if c.Prompt == "" {
		c.Prompt = strings.Join(others, "\n\n")
	}
	if c.Name == "" {
		c.Name = guessName(c.Prompt)
	}
	return c
}

var headingName = regexp.MustCompile(`(?m)^#{1,3}\s*(?:角色卡|角色|人设卡|人设)?\s*[:：]?\s*(.+?)\s*$`)

// guessName 从开头的 Markdown 标题猜角色名，例如「# 角色卡：DeepSeek 小鲸鱼 —— QQ 群友版」。
func guessName(text string) string {
	head := text
	if len(head) > 2000 {
		head = head[:2000]
	}
	if m := headingName.FindStringSubmatch(head); m != nil {
		return m[1]
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseCardJSON 解析 SillyTavern / TavernAI 角色卡（V1 平铺，或 V2/V3 放在 data 里）。
func parseCardJSON(b []byte) (Candidate, bool) {
	type card struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Personality  string `json:"personality"`
		Scenario     string `json:"scenario"`
		FirstMes     string `json:"first_mes"`
		MesExample   string `json:"mes_example"`
		SystemPrompt string `json:"system_prompt"`
		PostHistory  string `json:"post_history_instructions"`
		CreatorNotes string `json:"creator_notes"`
		Tags         []string
	}
	var raw struct {
		card
		Data *card `json:"data"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Candidate{}, false
	}
	c := raw.card
	if raw.Data != nil && raw.Data.Name != "" {
		c = *raw.Data
	}
	if c.Name == "" || (c.Description == "" && c.Personality == "" && c.SystemPrompt == "") {
		return Candidate{}, false
	}
	var p strings.Builder
	section := func(title, body string) {
		if body = strings.TrimSpace(body); body != "" {
			if p.Len() > 0 {
				p.WriteString("\n\n")
			}
			if title != "" {
				p.WriteString("## " + title + "\n")
			}
			p.WriteString(body)
		}
	}
	p.WriteString("# 角色：" + c.Name)
	section("", c.SystemPrompt)
	section("角色设定", c.Description)
	section("性格", c.Personality)
	section("场景", c.Scenario)
	section("说话示例", c.MesExample)
	section("开场白（第一次聊天时可以参考这样开口）", c.FirstMes)
	section("补充要求", c.PostHistory)
	text := replacePlaceholders(p.String(), c.Name)
	return Candidate{Name: c.Name, Description: firstLine(c.CreatorNotes), Tags: c.Tags, Prompt: text}, true
}

var placeholders = regexp.MustCompile(`(?i)\{\{\s*(char|user)\s*\}\}|<(bot|user)>`)

// replacePlaceholders 把角色卡里常见的 {{char}}、{{user}} 占位换成角色名和「对方」。
func replacePlaceholders(s, name string) string {
	return placeholders.ReplaceAllStringFunc(s, func(m string) string {
		if strings.Contains(strings.ToLower(m), "user") {
			return "对方"
		}
		return name
	})
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// ParseHTML 从网页里找角色卡：先找「卡片」（同一块里有标题和 <pre> 全文，例如 aigengtu.com/persona），
// 找不到时把每段较长的 <pre> 当作一张卡。
func (s *Service) ParseHTML(r io.Reader, source string) ([]Candidate, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	seen := map[*html.Node]bool{}
	walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || !isCard(n) {
			return true
		}
		pre := find(n, func(x *html.Node) bool { return x.Type == html.ElementNode && x.Data == "pre" })
		if pre == nil {
			return true
		}
		seen[pre] = true
		c := fromFiles(textOf(pre))
		if t := find(n, hasClass("title")); t != nil {
			c.Name = textOf(t)
		} else if h := find(n, isHeading); h != nil && c.Name == "" {
			c.Name = textOf(h)
		}
		if d := find(n, hasClass("desc")); d != nil {
			c.Description = textOf(d)
		}
		if tags := find(n, hasClass("tags")); tags != nil {
			for x := tags.FirstChild; x != nil; x = x.NextSibling {
				if t := strings.TrimSpace(textOf(x)); t != "" {
					c.Tags = append(c.Tags, t)
				}
			}
		}
		c.Source = source
		out = append(out, s.finish(c))
		return false
	})
	if len(out) > 0 {
		return out, nil
	}
	// 没有卡片结构：每段较长的 <pre> 算一张，名字用它前面最近的标题。
	var lastHeading string
	walk(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		if isHeading(n) {
			lastHeading = textOf(n)
			return false
		}
		if n.Data == "pre" && !seen[n] {
			text := textOf(n)
			if utf8.RuneCountInString(strings.TrimSpace(text)) >= 200 {
				c := fromFiles(text)
				if c.Name == "" {
					c.Name = lastHeading
				}
				c.Source = source
				out = append(out, s.finish(c))
			}
			return false
		}
		return true
	})
	if len(out) == 0 {
		return nil, &UserError{"这个网页里没找到角色提示词。可以把提示词全文复制下来，用「粘贴」导入"}
	}
	return out, nil
}

// walk 深度优先遍历，fn 返回 false 时不再进入这个节点的子节点。
func walk(n *html.Node, fn func(*html.Node) bool) {
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	var hit *html.Node
	walk(n, func(x *html.Node) bool {
		if hit != nil {
			return false
		}
		if x != n && match(x) {
			hit = x
			return false
		}
		return true
	})
	return hit
}

func classes(n *html.Node) []string {
	for _, a := range n.Attr {
		if a.Key == "class" {
			return strings.Fields(a.Val)
		}
	}
	return nil
}

// isCard 判断是不是一张卡片：class 以 card 结尾（ps-card、persona-card、card）的 article / div / section / li。
func isCard(n *html.Node) bool {
	switch n.Data {
	case "article", "div", "section", "li":
	default:
		return false
	}
	for _, c := range classes(n) {
		if c == "card" || strings.HasSuffix(c, "-card") {
			return true
		}
	}
	return false
}

func hasClass(suffix string) func(*html.Node) bool {
	return func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		for _, c := range classes(n) {
			if c == suffix || strings.HasSuffix(c, "-"+suffix) {
				return true
			}
		}
		return false
	}
}

func isHeading(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "h1", "h2", "h3", "h4":
		return true
	}
	return false
}

// textOf 返回节点里的文字（<pre> 里保留换行）。
func textOf(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) bool {
		switch {
		case x.Type == html.TextNode:
			b.WriteString(x.Data)
		case x.Type == html.ElementNode && (x.Data == "script" || x.Data == "style"):
			return false
		case x.Type == html.ElementNode && x.Data == "br":
			b.WriteString("\n")
		}
		return true
	})
	if n.Data == "pre" {
		return b.String()
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
