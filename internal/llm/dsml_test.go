package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractDSML(t *testing.T) {
	text := "先缓口气，回宿舍再动笔。六点我再喊你查慕课\n\n<｜DSML｜function_calls>\n<｜DSML｜invoke name=\"set_reminder\">\n" +
		"<｜DSML｜parameter name=\"content\" string=\"true\">查慕课</｜DSML｜parameter>\n" +
		"<｜DSML｜parameter name=\"after_minutes\" string=\"false\">150</｜DSML｜parameter>\n</｜DSML｜invoke>\n" +
		"<｜DSML｜invoke name=\"list_reminders\">\n</｜DSML｜invoke>\n</｜DSML｜function_calls>"
	clean, calls, found := ExtractDSML(text)
	if !found || clean != "先缓口气，回宿舍再动笔。六点我再喊你查慕课" || len(calls) != 2 {
		t.Fatalf("clean=%q calls=%+v", clean, calls)
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(calls[0].Arguments), &args)
	if calls[0].Name != "set_reminder" || args["content"] != "查慕课" || args["after_minutes"] != float64(150) {
		t.Fatalf("%+v %v", calls[0], args)
	}
	if calls[1].Name != "list_reminders" || calls[1].Arguments != "{}" {
		t.Fatalf("%+v", calls[1])
	}
	// 截图里的写法：竖线重复、夹空格，标签叫 calls。
	odd := "<｜ ｜DSML｜ ｜calls>\n<｜ ｜DSML｜ ｜invoke name=\"list_reminders\">\n</｜ ｜DSML｜ ｜invoke>\n</｜ ｜DSML｜ ｜calls>"
	if clean, calls, found := ExtractDSML(odd); !found || clean != "" || len(calls) != 1 || calls[0].Name != "list_reminders" {
		t.Fatalf("clean=%q calls=%+v", clean, calls)
	}
	if _, _, found := ExtractDSML("正常的回复 a<b 也没关系"); found {
		t.Fatal("误判")
	}
}

func TestDSMLFilter(t *testing.T) {
	var f DSMLFilter
	var out strings.Builder
	for _, d := range []string{"六点", "我再喊你", "\n\n<", "｜DS", "ML｜invoke name=\"x\">", "后面都不要"} {
		out.WriteString(f.Push(d))
	}
	out.WriteString(f.Flush())
	if out.String() != "六点我再喊你\n\n" || !f.Hit() {
		t.Fatalf("%q", out.String())
	}
	var g DSMLFilter
	out.Reset()
	for _, d := range []string{"1<2 而且", " 3>2", "，结束 <"} {
		out.WriteString(g.Push(d))
	}
	out.WriteString(g.Flush())
	if out.String() != "1<2 而且 3>2，结束 <" || g.Hit() {
		t.Fatalf("%q", out.String())
	}
}
