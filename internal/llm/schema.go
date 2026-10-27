package llm

// CleanSchema 整理工具参数的 JSON Schema，免得较严格的模型服务拒绝整个请求：
// 去掉值为 null 的字段（例如没有必填参数时的 "required": null）和空的 required，
// 没写 type 时当作 object，object 没有 properties 时补一个空的。
// 不修改传入的 map（MCP 插件的 schema 是共用的）。
func CleanSchema(s map[string]any) map[string]any {
	out := cleanMap(s)
	if out == nil {
		out = map[string]any{}
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	if out["type"] == "object" {
		if _, ok := out["properties"].(map[string]any); !ok {
			out["properties"] = map[string]any{}
		}
	}
	return out
}

func cleanMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		v = cleanValue(v)
		if v == nil {
			continue
		}
		if k == "required" {
			if empty(v) {
				continue
			}
		}
		out[k] = v
	}
	return out
}

func cleanValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		if x == nil {
			return nil
		}
		return cleanMap(x)
	case []any:
		if x == nil {
			return nil
		}
		out := make([]any, 0, len(x))
		for _, e := range x {
			if e = cleanValue(e); e != nil {
				out = append(out, e)
			}
		}
		return out
	case []string:
		if x == nil {
			return nil
		}
		return x
	case []map[string]any:
		if x == nil {
			return nil
		}
		out := make([]any, 0, len(x))
		for _, e := range x {
			if c := cleanMap(e); c != nil {
				out = append(out, c)
			}
		}
		return out
	}
	return v
}

func empty(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	}
	return false
}
