package tools

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

// newStdout 返回标准输出的缓冲区：除了文字输出，还要装下附在末尾的图片。
func newStdout() *limitedBuffer {
	return &limitedBuffer{n: maxOutputBytes + maxImagesTotal*4/3 + 64<<10}
}

// splitImages 从标准输出末尾取出图片，返回剩下的文字输出（不超过 maxOutputBytes）。
func splitImages(out *limitedBuffer, boundary string) (string, []Image, int) {
	raw := out.buf.Bytes()
	var images []Image
	skipped := 0
	marker := []byte("\n" + boundary + "\n")
	if i := bytes.LastIndex(raw, marker); i >= 0 {
		var payload struct {
			Images []struct {
				Name string `json:"name"`
				Data string `json:"data"`
			} `json:"images"`
			Skipped int `json:"skipped"`
		}
		if json.Unmarshal(raw[i+len(marker):], &payload) == nil {
			skipped = payload.Skipped
			for _, img := range payload.Images {
				data, err := base64.StdEncoding.DecodeString(img.Data)
				if err != nil || len(data) == 0 || len(data) > maxImageBytes || len(images) >= maxImages {
					skipped++
					continue
				}
				images = append(images, Image{Name: filepath.Base(img.Name), Data: data})
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
	return text, images, skipped
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
