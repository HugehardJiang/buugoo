package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// DeepSeek 有时不按接口返回结构化的 tool_calls，而是把它内部的工具调用标记（DSML）直接写进正文，例如：
//
//	<｜DSML｜function_calls>
//	<｜DSML｜invoke name="list_reminders">
//	<｜DSML｜parameter name="kind" string="true">remind</｜DSML｜parameter>
//	</｜DSML｜invoke>
//	</｜DSML｜function_calls>
//
// 这些文字不能发给用户。ExtractDSML 把它们从正文里去掉，并尽量还原成真正的工具调用。
// 竖线可能是全角「｜」或半角「|」，也可能重复或夹空格，所以都按宽松的写法匹配。

const bars = `[｜|\s]*`

var (
	dsmlStartRe  = regexp.MustCompile(`<` + bars + `DSML` + bars)
	dsmlInvokeRe = regexp.MustCompile(`(?s)<` + bars + `DSML` + bars + `invoke\s+name\s*=\s*"([^"]+)"\s*>(.*?)(?:</` + bars + `DSML` + bars + `invoke\s*>|$)`)
	dsmlParamRe  = regexp.MustCompile(`(?s)<` + bars + `DSML` + bars + `parameter\s+name\s*=\s*"([^"]+)"((?:\s+\w+\s*=\s*"[^"]*")*)\s*>(.*?)</` + bars + `DSML` + bars + `parameter\s*>`)
	dsmlStringRe = regexp.MustCompile(`string\s*=\s*"(true|false)"`)
)

// DSMLStart 返回正文里 DSML 标记开始的位置，没有时返回 -1。
func DSMLStart(text string) int {
	if loc := dsmlStartRe.FindStringIndex(text); loc != nil {
		return loc[0]
	}
	return -1
}

// ExtractDSML 去掉正文里的 DSML 工具调用标记（从第一个标记到末尾），返回剩下的正文和还原出来的工具调用。
// found 表示正文里有 DSML 标记。
func ExtractDSML(text string) (clean string, calls []ToolCall, found bool) {
	start := DSMLStart(text)
	if start < 0 {
		return text, nil, false
	}
	block := text[start:]
	for i, m := range dsmlInvokeRe.FindAllStringSubmatch(block, -1) {
		args := map[string]any{}
		for _, p := range dsmlParamRe.FindAllStringSubmatch(m[2], -1) {
			name, attrs, raw := p[1], p[2], strings.TrimSpace(p[3])
			isString := true
			if s := dsmlStringRe.FindStringSubmatch(attrs); s != nil {
				isString = s[1] == "true"
			}
			var v any = raw
			if !isString {
				if err := json.Unmarshal([]byte(raw), &v); err != nil {
					v = raw
				}
			}
			args[name] = v
		}
		b, _ := json.Marshal(args)
		calls = append(calls, ToolCall{ID: fmt.Sprintf("dsml_%d", i), Name: strings.TrimSpace(m[1]), Arguments: string(b)})
	}
	return strings.TrimRight(text[:start], " \t\n"), calls, true
}

// DSMLFilter 在流式输出时拦住 DSML 标记：标记之前的正文照常推送，从标记开始的内容都不推送。
// 可能是标记开头的一小段（例如「<｜DS」）会先压着，等后面的字到了再决定。
type DSMLFilter struct {
	held string
	hit  bool
}

// Push 接收一段增量，返回现在可以推送给用户的部分。
func (f *DSMLFilter) Push(delta string) string {
	if f.hit {
		return ""
	}
	s := f.held + delta
	f.held = ""
	if i := DSMLStart(s); i >= 0 {
		f.hit = true
		return s[:i]
	}
	// 最后一个「<」之后还没有「>」、又不长：可能是标记的开头，先压着。
	if i := strings.LastIndex(s, "<"); i >= 0 && len(s)-i < 24 && !strings.Contains(s[i:], ">") {
		f.held = s[i:]
		return s[:i]
	}
	return s
}

// Flush 在输出结束时返回压着的内容（遇到了标记时为空）。
func (f *DSMLFilter) Flush() string {
	if f.hit {
		return ""
	}
	s := f.held
	f.held = ""
	return s
}

// Hit 报告是否遇到了 DSML 标记。
func (f *DSMLFilter) Hit() bool { return f.hit }
