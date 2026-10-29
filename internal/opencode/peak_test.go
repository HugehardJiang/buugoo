package opencode

import (
	"strings"
	"testing"
	"time"
)

func TestOpenCodePeak(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	r := PeakRule(sh)
	if r == nil || strings.Join(r.Ranges, ",") != "09:00-12:00,14:00-18:00" || len(r.Days) != 5 {
		t.Fatalf("%+v", r)
	}
	ny, _ := time.LoadLocation("America/New_York")
	if PeakRule(ny) != nil {
		t.Fatal("纽约时间跨天，不能用每天的时段表示")
	}
}
