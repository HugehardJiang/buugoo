// Package timefmt 提供中文时间格式化。
package timefmt

import "time"

var weekdays = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

// Weekday 返回中文星期。
func Weekday(t time.Time) string { return weekdays[t.Weekday()] }

// Full 返回形如"2026-10-25 14:30 星期日"的时间。
func Full(t time.Time) string { return t.Format("2006-01-02 15:04") + " " + Weekday(t) }

// Date 返回形如"2026-10-25"的日期。
func Date(t time.Time) string { return t.Format("2006-01-02") }
