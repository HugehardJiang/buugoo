package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// ErrChecksumMismatch 表示下载文件的 SHA256 与校验文件不符。
var ErrChecksumMismatch = errors.New("SHA256 校验失败")

// ParseChecksums 解析 GoReleaser 生成的 checksums.txt（每行"<sha256>  <文件名>"）。
func ParseChecksums(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		sum, name := strings.ToLower(fields[0]), strings.TrimPrefix(fields[1], "*")
		if len(sum) != 64 {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}
		out[name] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("校验文件为空或格式不正确")
	}
	return out, nil
}

// VerifyFile 校验文件的 SHA256。
func VerifyFile(file, want string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w：期望 %s，实际 %s", ErrChecksumMismatch, want, got)
	}
	return nil
}

// ExtractBinary 从发布包中取出 buugoo（或 buugoo.exe）写到 dst。
func ExtractBinary(archive, dst string) error {
	name := "buugoo"
	if strings.HasSuffix(archive, ".zip") {
		name = "buugoo.exe"
		return extractZip(archive, name, dst)
	}
	return extractTarGz(archive, name, dst)
}

func extractTarGz(archive, name, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解压 %s: %w", archive, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("发布包中没有找到 %s", name)
		}
		if err != nil {
			return fmt.Errorf("解压 %s: %w", archive, err)
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == name {
			return writeExecutable(dst, tr)
		}
	}
}

func extractZip(archive, name, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("解压 %s: %w", archive, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if path.Base(f.Name) != name || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeExecutable(dst, rc)
	}
	return fmt.Errorf("发布包中没有找到 %s", name)
}

func writeExecutable(dst string, r io.Reader) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	// 发布包不会超过 200MB，限制大小防止异常文件写满磁盘。
	if _, err := io.Copy(out, io.LimitReader(r, 200<<20)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyFile 复制文件并保留权限。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
