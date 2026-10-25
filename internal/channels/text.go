package channels

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxBubbles 是一次回复最多拆成的消息条数；段落再多就合成一条（与网页的拆分规则一致）。
const maxBubbles = 6

// structured 匹配代码块、公式、列表、表格、标题、引用这类成块的内容，它们和前一段放在同一条消息里。
var structured = regexp.MustCompile("^(```|~~~|\\$\\$|\\\\\\[|\\||#{1,6}\\s|[-*+]\\s|\\d+[.、)]\\s?|>)")

// SplitBubbles 把回复按空行拆成几条消息，像真人在微信上那样一条条发；代码块与公式块内部的空行不拆。
func SplitBubbles(text string) []string {
	var paras []string
	var cur []string
	fence, math := "", false
	flush := func() {
		if p := strings.TrimSpace(strings.Join(cur, "\n")); p != "" {
			paras = append(paras, p)
		}
		cur = nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
		case math:
			if strings.HasSuffix(t, "$$") || strings.HasSuffix(t, `\]`) {
				math = false
			}
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			fence = t[:3]
		case t == "$$" || (strings.HasPrefix(t, "$$") && !strings.Contains(t[2:], "$$")) ||
			(strings.HasPrefix(t, `\[`) && !strings.Contains(t, `\]`)):
			math = true
		case t == "":
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	var out []string
	for _, p := range paras {
		if len(out) > 0 && structured.MatchString(p) {
			out[len(out)-1] += "\n\n" + p
		} else {
			out = append(out, p)
		}
	}
	if len(out) > maxBubbles {
		return []string{strings.Join(out, "\n\n")}
	}
	return out
}

var (
	reBold    = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	reCode    = regexp.MustCompile("`([^`\n]+)`")
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	reHeading = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reFence   = regexp.MustCompile("(?m)^(```|~~~)[a-zA-Z0-9_+-]*\\s*$")
)

// PlainText 把 Markdown 变成聊天软件里好读的纯文本：去掉加粗、行内代码与标题记号，链接写成「文字 网址」。
func PlainText(s string) string {
	s = reFence.ReplaceAllString(s, "")
	s = reBold.ReplaceAllStringFunc(s, func(m string) string { return m[2 : len(m)-2] })
	s = reCode.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1 $2")
	s = reHeading.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// typingDelay 是发下一条消息前停顿的时间（毫秒），模拟打字：越长停得越久，最多 2.5 秒。
func typingDelay(text string) int {
	return min(400+utf8.RuneCountInString(text)*60, 2500)
}
