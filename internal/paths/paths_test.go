package paths

import (
	"path/filepath"
	"testing"
)

func TestLayoutFor(t *testing.T) {
	noenv := func(string) string { return "" }
	cases := []struct {
		goos, scope string
		getenv      func(string) string
		want        Layout
	}{
		{"linux", ScopeSystem, noenv, Layout{ScopeSystem, "/usr/local/bin", "/etc/buugoo", "/var/lib/buugoo"}},
		{"linux", ScopeUser, noenv, Layout{ScopeUser, "/home/u/.local/bin", "/home/u/.config/buugoo", "/home/u/.local/share/buugoo"}},
		{"linux", ScopeUser, func(k string) string {
			return map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_DATA_HOME": "/x/data"}[k]
		}, Layout{ScopeUser, "/home/u/.local/bin", "/x/cfg/buugoo", "/x/data/buugoo"}},
		{"darwin", ScopeSystem, noenv, Layout{ScopeSystem, "/usr/local/bin", "/usr/local/etc/buugoo", "/usr/local/var/buugoo"}},
	}
	for _, tc := range cases {
		got, err := layoutFor(tc.goos, tc.scope, "/home/u", tc.getenv)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s/%s: got %+v, want %+v", tc.goos, tc.scope, got, tc.want)
		}
	}
	if _, err := layoutFor("linux", "weird", "/home/u", noenv); err == nil {
		t.Error("未知范围应报错")
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{Layout: Layout{Scope: ScopeUser, ConfigDir: dir}, Binary: "/x/buugoo", Version: "v1.0.0"}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManifest(filepath.Join(dir, ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if got.Binary != m.Binary || got.Version != m.Version || got.ConfigDir != dir {
		t.Errorf("round trip mismatch: %+v", got)
	}
}
