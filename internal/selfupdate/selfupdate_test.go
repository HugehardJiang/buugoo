package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func TestVersions(t *testing.T) {
	if v, err := Normalize("1.2.3"); err != nil || v != "v1.2.3" {
		t.Fatal(v, err)
	}
	if _, err := Normalize("latest"); !errors.Is(err, ErrInvalidVersion) {
		t.Fatal(err)
	}
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.2.0", "v1.2.0", false},
		{"v1.2.0", "v1.10.0", false},
		{"v1.2.0", "v1.2.0-rc.1", true},
		{"v0.1.0", "dev", true},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%s,%s)=%v", c.latest, c.current, got)
		}
	}
	if AssetName("v1.2.3", "linux", "arm64") != "buugoo_1.2.3_linux_arm64.tar.gz" ||
		AssetName("v1.2.3", "windows", "amd64") != "buugoo_1.2.3_windows_amd64.zip" {
		t.Fatal("asset name")
	}
}

func TestSourceMirrorAndGitHub(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			fmt.Fprint(w, "v1.4.0\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer mirror.Close()
	s := NewSource("", mirror.URL+"/")
	v, err := s.Latest(context.Background())
	if err != nil || v != "v1.4.0" {
		t.Fatal(v, err)
	}
	if got := s.URL("v1.4.0", "checksums.txt"); got != mirror.URL+"/v1.4.0/checksums.txt" {
		t.Fatal(got)
	}
	gh := NewSource("a/b", "")
	if gh.Mirror == "" && gh.URL("v1.0.0", "x.tar.gz") != "https://github.com/a/b/releases/download/v1.0.0/x.tar.gz" {
		t.Fatal("github url")
	}
}

func TestChecksumsAndExtract(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("#!/bin/sh\necho hi\n")

	tgz := filepath.Join(dir, "buugoo_1.0.0_linux_amd64.tar.gz")
	f, _ := os.Create(tgz)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "LICENSE", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("ok"))
	_ = tw.WriteHeader(&tar.Header{Name: "buugoo", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(payload)
	tw.Close()
	gz.Close()
	f.Close()

	zp := filepath.Join(dir, "buugoo_1.0.0_windows_amd64.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("buugoo.exe")
	_, _ = w.Write(payload)
	zw.Close()
	zf.Close()

	sum := func(p string) string {
		b, _ := os.ReadFile(p)
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	sums, err := ParseChecksums(strings.NewReader(sum(tgz) + "  buugoo_1.0.0_linux_amd64.tar.gz\n" + sum(zp) + "  buugoo_1.0.0_windows_amd64.zip\njunk line\n"))
	if err != nil || len(sums) != 2 {
		t.Fatal(sums, err)
	}
	if err := VerifyFile(tgz, sums["buugoo_1.0.0_linux_amd64.tar.gz"]); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(tgz, strings.Repeat("0", 64)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("篡改后应校验失败: %v", err)
	}
	for _, a := range []string{tgz, zp} {
		out := filepath.Join(dir, "out-"+filepath.Base(a))
		if err := ExtractBinary(a, out); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(out)
		if string(got) != string(payload) {
			t.Fatalf("%s: 解压内容不一致", a)
		}
	}
	if _, err := ParseChecksums(strings.NewReader("")); err == nil {
		t.Fatal("空校验文件应报错")
	}
}

func TestSQLiteBackupRestore(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Database.DSN = filepath.Join(cfg.DataDir, "buugoo.db")
	db, err := gormstore.Open(cfg.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Meta().Set(ctx, "k", "before"); err != nil {
		t.Fatal(err)
	}
	b, err := BackupDatabase(ctx, cfg, "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != BackupSQLite || !strings.HasPrefix(b.Path, filepath.Join(cfg.DataDir, "backups")) {
		t.Fatalf("backup=%+v", b)
	}
	if err := db.Meta().Set(ctx, "k", "after"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := RestoreSQLite(b); err != nil {
		t.Fatal(err)
	}
	db2, err := gormstore.Open(cfg.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if v, _, _ := db2.Meta().Get(ctx, "k"); v != "before" {
		t.Fatalf("恢复后应为备份时的数据: %q", v)
	}

	// 尚无数据库时无需备份。
	empty := config.Default()
	empty.DataDir = t.TempDir()
	empty.Database.DSN = filepath.Join(empty.DataDir, "none.db")
	if b, err := BackupDatabase(ctx, empty, "x", nil); err != nil || b.Kind != BackupNone {
		t.Fatal(b, err)
	}
	// MySQL 且没有 mysqldump、用户拒绝时取消更新。
	my := config.Default()
	my.DataDir = t.TempDir()
	my.Database.Driver = config.DriverMySQL
	my.Database.DSN = "u:p@tcp(127.0.0.1:3306)/db"
	t.Setenv("PATH", t.TempDir())
	if _, err := BackupDatabase(ctx, my, "x", func(string) bool { return false }); !errors.Is(err, ErrBackupDeclined) {
		t.Fatal(err)
	}
}

func TestHealthURL(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"127.0.0.1:8080": "http://127.0.0.1:8080/healthz",
		":8081":          "http://127.0.0.1:8081/healthz",
	} {
		if got := healthURL(in); got != want {
			t.Errorf("healthURL(%q)=%q", in, got)
		}
	}
}
