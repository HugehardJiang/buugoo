package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	dir := t.TempDir()
	b, src, err := Load("", dir)
	if err != nil || src != filepath.Join(dir, "secret.key") {
		t.Fatal(src, err)
	}
	st, _ := os.Stat(src)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("密钥文件权限应为 0600: %v", st.Mode())
	}
	sealed, err := b.Seal("sk-secret-123456")
	if err != nil || strings.Contains(sealed, "secret") {
		t.Fatalf("sealed=%q err=%v", sealed, err)
	}
	if got, err := b.Open(sealed); err != nil || got != "sk-secret-123456" {
		t.Fatal(got, err)
	}
	// 再次加载使用同一密钥。
	b2, _, _ := Load("", dir)
	if got, _ := b2.Open(sealed); got != "sk-secret-123456" {
		t.Fatal("重新加载后应能解密")
	}
	other, _, err := Load(strings.Repeat("ab", 32), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed); !errors.Is(err, ErrDecrypt) {
		t.Fatal("换了密钥应无法解密")
	}
	if _, _, err := Load("zz", ""); err == nil {
		t.Fatal("无效密钥应报错")
	}
	if s, _ := b.Seal(""); s != "" {
		t.Fatal("空值不加密")
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"": "", "abc": "••••", "sk-1234567890abcd": "sk-…abcd", "AKIA12345678WXYZ": "AK…WXYZ"} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q)=%q want %q", in, got, want)
		}
	}
}
