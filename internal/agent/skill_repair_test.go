package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/skills"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 安装 skill 时卡片上有「允许联网」的开关，用户在确认时可以改；修 skill 时改文件要先确认。
func TestSkillInstallNetworkAndRepair(t *testing.T) {
	s, fake, u, _, db := setup(t)
	ctx := context.Background()
	s.d.Skills = skills.New(db.Gorm(), filepath.Join(t.TempDir(), "skills"), nil)

	md := "---\nname: weather\ndescription: 查天气\n---\n运行 `curl -s https://wttr.in/北京`"
	fake.call("install_skill", map[string]any{"content": md, "network": false})
	fake.say("请确认")
	reply, err := s.Send(ctx, u, "装一个查天气的", true)
	if err != nil {
		t.Fatal(err)
	}
	a := reply.Actions[0]
	// 牛马说不用联网，但服务器检测到 curl：默认勾上，并说明依据。
	if a.Option == nil || a.Option.Key != "network" || !a.Option.Value || !strings.Contains(a.Summary, "curl") {
		t.Fatalf("card = %+v", a)
	}
	// 用户取消勾选再确认：装好，但不能联网。
	done, err := s.Confirm(ctx, u, a.ID, true, map[string]bool{"network": false})
	if err != nil || done.Status != store.ActionDone {
		t.Fatalf("confirm = %+v %v", done, err)
	}
	sk, err := s.d.Skills.ByName(ctx, "weather")
	if err != nil || sk.AllowNetwork {
		t.Fatalf("skill = %+v %v", sk, err)
	}

	// 修 skill：先看文件，再提出修改，卡片里列出改动。
	fake.call("inspect_skill", map[string]any{"name": "weather"})
	fake.call("edit_skill_file", map[string]any{"name": "weather", "path": "SKILL.md", "reason": "换成能访问的地址",
		"content": strings.Replace(md, "wttr.in/北京", "wttr.in/Beijing?format=3", 1)})
	fake.say("请确认")
	reply, err = s.Send(ctx, u, "weather 这个 skill 坏了，帮我修", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Actions) != 1 || !strings.Contains(reply.Actions[0].Summary, "+ 运行 `curl -s https://wttr.in/Beijing?format=3`") {
		t.Fatalf("reply = %+v", reply)
	}
	if b, _ := s.d.Skills.ReadFile("weather", "SKILL.md"); strings.Contains(string(b), "Beijing") {
		t.Fatal("确认前不应修改")
	}
	if done, err := s.Confirm(ctx, u, reply.Actions[0].ID, true, nil); err != nil || done.Status != store.ActionDone {
		t.Fatalf("confirm edit = %+v %v", done, err)
	}
	if b, _ := s.d.Skills.ReadFile("weather", "SKILL.md"); !strings.Contains(string(b), "Beijing") {
		t.Fatalf("SKILL.md = %s", b)
	}
}
