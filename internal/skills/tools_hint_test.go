package skills

import (
	"strings"
	"testing"
)

func TestNetworkHint(t *testing.T) {
	out := `{"ok": false, "error": "<urlopen error [Errno -3] Temporary failure in name resolution>"}`
	if h := networkHint(false, out); !strings.Contains(h, "脚本可联网") {
		t.Fatal(h)
	}
	if h := networkHint(true, out); !strings.Contains(h, "Docker") {
		t.Fatal(h)
	}
	if h := networkHint(false, "晴，16℃"); h != "" {
		t.Fatal(h)
	}
}
