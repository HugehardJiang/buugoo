package skills

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

// 判断 skill 的脚本要不要联网：看脚本里有没有网址、有没有用联网的库或命令。只是推测，最后由管理员决定。

var (
	urlRe = regexp.MustCompile(`https?://[A-Za-z0-9.\-]+(?::\d+)?`)
	// netLibs 是常见的联网库和命令。
	netLibs = []struct {
		re   *regexp.Regexp
		name string
	}{
		{regexp.MustCompile(`(?m)^\s*(import|from)\s+(requests|httpx|aiohttp|urllib3?|http\.client)\b`), "Python 联网库"},
		{regexp.MustCompile(`urllib\.request|urlopen\(`), "urllib"},
		{regexp.MustCompile(`\b(fetch|axios)\s*\(`), "fetch / axios"},
		{regexp.MustCompile("(?m)(^|[\\s;|&(`$])(curl|wget)\\s"), "curl / wget"},
	}
	// 只看脚本和说明，不看图片、数据之类的文件。
	scriptExt = map[string]bool{".py": true, ".sh": true, ".bash": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".rb": true, ".md": true}
)

// ignoredHosts 是说明里常见、但运行时不会访问的网址（文档、仓库、示例）。
var ignoredHosts = map[string]bool{"github.com": true, "raw.githubusercontent.com": true, "example.com": true, "www.example.com": true,
	"localhost": true, "127.0.0.1": true, "opensource.org": true, "www.apache.org": true, "docs.python.org": true, "pypi.org": true}

// detectNetwork 推测 dir 下的 skill 要不要联网，要的话返回依据（例如「scripts/a.py 会访问 www.nmc.cn」），不要返回空。
func detectNetwork(t tree, dir string) string {
	dir = strings.Trim(dir, "/")
	var names []string
	for p := range t {
		if dir == "" || strings.HasPrefix(p, dir+"/") {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	var hosts []string
	seen := map[string]bool{}
	lib := ""
	for _, p := range names {
		ext := strings.ToLower(path.Ext(p))
		if !scriptExt[ext] || len(t[p]) > 1<<20 {
			continue
		}
		text := string(t[p])
		rel := strings.TrimPrefix(p, dir+"/")
		isScript := ext != ".md"
		for _, m := range urlRe.FindAllString(text, -1) {
			u, err := url.Parse(m)
			if err != nil || ignoredHosts[u.Hostname()] || seen[u.Hostname()] {
				continue
			}
			// 说明文档里的网址多半是链接，只有脚本里的才算。
			if !isScript {
				continue
			}
			seen[u.Hostname()] = true
			hosts = append(hosts, u.Hostname())
		}
		if lib == "" {
			for _, l := range netLibs {
				if (isScript || l.name == "curl / wget") && l.re.MatchString(text) {
					lib = fmt.Sprintf("%s 用到了 %s", rel, l.name)
					break
				}
			}
		}
	}
	switch {
	case len(hosts) > 0:
		if len(hosts) > 3 {
			hosts = append(hosts[:3], "等")
		}
		s := "脚本会访问 " + strings.Join(hosts, "、")
		if lib != "" {
			s += "（" + lib + "）"
		}
		return s
	case lib != "":
		return lib
	}
	return ""
}

// DetectText 推测只有 SKILL.md 的 skill 要不要联网。
func DetectText(md string) string { return detectNetwork(tree{"SKILL.md": []byte(md)}, "") }
