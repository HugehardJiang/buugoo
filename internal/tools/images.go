package tools

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 代码生成图片的上限。
const (
	maxImages      = 6
	maxImageBytes  = 5 << 20
	maxImagesTotal = 16 << 20
)

// mplCacheDir 是库目录中 matplotlib 字体缓存的位置。
const mplCacheDir = ".matplotlib"

// hostFonts 是服务器的字体目录，只读挂载进沙箱，让图表可以使用服务器上装的中文字体。
const hostFonts = "/usr/share/fonts"

// Image 是代码生成的一张图片。
type Image struct {
	Name string
	Data []byte
}

//go:embed runner.py
var runnerScript string

// wrapCode 把用户代码包进收集图片的脚本，返回脚本与本次运行的分隔符。
func wrapCode(code string) (script, boundary string) {
	var rnd [16]byte
	_, _ = rand.Read(rnd[:])
	boundary = "--buugoo-images-" + hex.EncodeToString(rnd[:])
	script = strings.NewReplacer(
		"{{BOUNDARY}}", boundary,
		"{{CODE}}", base64.StdEncoding.EncodeToString([]byte(code)),
		"{{MAX_IMAGES}}", strconv.Itoa(maxImages),
		"{{MAX_EACH}}", strconv.Itoa(maxImageBytes),
		"{{MAX_TOTAL}}", strconv.Itoa(maxImagesTotal),
	).Replace(runnerScript)
	return script, boundary
}

// newStdout 返回标准输出的缓冲区：除了文字输出，还要装下附在末尾的图片与工作目录。
func newStdout(workspaceMax int) *limitedBuffer {
	return &limitedBuffer{n: maxOutputBytes + (maxImagesTotal+workspaceMax)*4/3 + 64<<10}
}

// runTail 是代码运行结束后附在标准输出末尾的内容。
type runTail struct {
	Images  []Image
	Skipped int
	// Workspace 是沙箱中工作目录有变化时打包的 tar.gz，为空表示没有变化。
	Workspace []byte
	// WorkspaceErr 说明工作目录没能交回的原因（例如超过大小上限）。
	WorkspaceErr string
}

// splitTail 从标准输出末尾取出图片与工作目录，返回剩下的文字输出（不超过 maxOutputBytes）。
func splitTail(out *limitedBuffer, boundary string) (string, runTail) {
	raw := out.buf.Bytes()
	var t runTail
	marker := []byte("\n" + boundary + "\n")
	if i := bytes.LastIndex(raw, marker); i >= 0 {
		var payload struct {
			Images []struct {
				Name string `json:"name"`
				Data string `json:"data"`
			} `json:"images"`
			Skipped   int `json:"skipped"`
			Workspace *struct {
				Data  string `json:"data"`
				Error string `json:"error"`
				Size  int    `json:"size"`
			} `json:"workspace"`
		}
		if json.Unmarshal(raw[i+len(marker):], &payload) == nil {
			t.Skipped = payload.Skipped
			for _, img := range payload.Images {
				data, err := base64.StdEncoding.DecodeString(img.Data)
				if err != nil || len(data) == 0 || len(data) > maxImageBytes || len(t.Images) >= maxImages {
					t.Skipped++
					continue
				}
				t.Images = append(t.Images, Image{Name: filepath.Base(img.Name), Data: data})
			}
			if w := payload.Workspace; w != nil {
				switch {
				case w.Error == "too_large":
					t.WorkspaceErr = fmt.Sprintf("工作目录打包后有 %d MB，超过上限，这次的改动没有保存", w.Size>>20)
				case w.Error != "":
					t.WorkspaceErr = "保存工作目录失败：" + w.Error
				default:
					if data, err := base64.StdEncoding.DecodeString(w.Data); err == nil {
						t.Workspace = data
					}
				}
			}
		}
		raw = raw[:i]
	}
	cut := out.cut
	if len(raw) > maxOutputBytes {
		raw, cut = raw[:maxOutputBytes], true
	}
	text := string(raw)
	if cut {
		text += "\n…（输出过长，已截断）"
	}
	return text, t
}

var (
	cjkOnce  sync.Once
	cjkFound bool
)

// HasCJKFont 报告服务器上是否装了中文字体（决定图表里能不能写中文）。
func HasCJKFont() bool {
	cjkOnce.Do(func() {
		keys := []string{"cjk", "wqy", "wenquanyi", "sourcehan", "simhei", "simsun", "msyh", "notosanssc", "notoserifsc", "droidsansfallback"}
		_ = filepath.WalkDir(hostFonts, func(path string, d fs.DirEntry, err error) error {
			if err != nil || cjkFound {
				return fs.SkipAll
			}
			name := strings.ToLower(strings.ReplaceAll(d.Name(), "-", ""))
			for _, k := range keys {
				if strings.Contains(name, k) {
					cjkFound = true
					return fs.SkipAll
				}
			}
			return nil
		})
	})
	return cjkFound
}

// fontArgs 把服务器的字体目录只读挂载进容器。
func (d *Docker) fontArgs() []string {
	if info, err := os.Stat(hostFonts); err == nil && info.IsDir() {
		return []string{"-v", hostFonts + ":" + hostFonts + ":ro"}
	}
	return nil
}
