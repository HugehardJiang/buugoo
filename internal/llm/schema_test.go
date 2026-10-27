package llm

import (
	"encoding/json"
	"testing"
)

func TestCleanSchema(t *testing.T) {
	var noReq []string
	in := map[string]any{"type": "object", "properties": map[string]any{
		"a": map[string]any{"type": "string", "enum": nil},
		"b": map[string]any{"type": "object", "properties": map[string]any{}, "required": noReq},
	}, "required": noReq}
	b, _ := json.Marshal(CleanSchema(in))
	if got := string(b); got != `{"properties":{"a":{"type":"string"},"b":{"properties":{},"type":"object"}},"type":"object"}` {
		t.Fatal(got)
	}
	if _, ok := in["required"]; !ok {
		t.Fatal("不应修改传入的 schema")
	}
	b, _ = json.Marshal(CleanSchema(nil))
	if string(b) != `{"properties":{},"type":"object"}` {
		t.Fatal(string(b))
	}
	b, _ = json.Marshal(CleanSchema(map[string]any{"type": "object", "required": []string{"x"}}))
	if string(b) != `{"properties":{},"required":["x"],"type":"object"}` {
		t.Fatal(string(b))
	}
}
