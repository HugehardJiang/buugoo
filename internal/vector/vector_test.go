package vector

import (
	"errors"
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestEncodeDecodeRoundTrip(t *testing.T) {
	v := []float32{0, 1, -1, 3.14159, float32(math.SmallestNonzeroFloat32), math.MaxFloat32}
	got, err := Decode(Encode(v))
	if err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if got[i] != v[i] {
			t.Fatalf("index %d: %v != %v", i, got[i], v[i])
		}
	}
	// 小端序：1.0 = 0x3f800000
	if b := Encode([]float32{1}); b[0] != 0 || b[3] != 0x3f {
		t.Fatalf("应为小端序: %x", b)
	}
	if _, err := Decode([]byte{1, 2, 3}); !errors.Is(err, ErrInvalidBytes) {
		t.Fatal(err)
	}
	if v, err := Decode(nil); err != nil || len(v) != 0 {
		t.Fatal("空字节应解码为空向量")
	}
}

func TestNormalize(t *testing.T) {
	n, err := Normalize([]float32{3, 4})
	if err != nil || !approx(float64(n[0]), 0.6) || !approx(float64(n[1]), 0.8) {
		t.Fatalf("n=%v err=%v", n, err)
	}
	if _, err := Normalize([]float32{0, 0}); !errors.Is(err, ErrZeroVector) {
		t.Fatal(err)
	}
	if _, err := Normalize([]float32{float32(math.NaN())}); !errors.Is(err, ErrNotFinite) {
		t.Fatal(err)
	}
	src := []float32{1, 1}
	_, _ = Normalize(src)
	if src[0] != 1 {
		t.Fatal("Normalize 不应修改输入")
	}
}

func TestCosineAndDot(t *testing.T) {
	cases := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1},
		{[]float32{1, 0}, []float32{0, 1}, 0},
		{[]float32{1, 0}, []float32{-1, 0}, -1},
		{[]float32{1, 1}, []float32{2, 2}, 1},
		{[]float32{1, 2, 3}, []float32{4, 5, 6}, 32 / (math.Sqrt(14) * math.Sqrt(77))},
		{[]float32{0, 0}, []float32{1, 1}, 0},
	}
	for _, tc := range cases {
		got, err := Cosine(tc.a, tc.b)
		if err != nil || !approx(got, tc.want) {
			t.Errorf("Cosine(%v,%v)=%v want %v err=%v", tc.a, tc.b, got, tc.want, err)
		}
		// 归一化后点积应等于余弦相似度。
		na, e1 := Normalize(tc.a)
		nb, e2 := Normalize(tc.b)
		if e1 == nil && e2 == nil {
			d, _ := Dot(na, nb)
			if !approx(d, tc.want) {
				t.Errorf("Dot(normalized)=%v want %v", d, tc.want)
			}
		}
	}
	if _, err := Cosine([]float32{1}, []float32{1, 2}); !errors.Is(err, ErrDimMismatch) {
		t.Fatal(err)
	}
	if _, err := Dot([]float32{1}, []float32{1, 2}); !errors.Is(err, ErrDimMismatch) {
		t.Fatal(err)
	}
}
