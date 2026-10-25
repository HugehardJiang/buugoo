#!/usr/bin/env python3
"""从设计系统的 tokens.json 生成 web/src/tokens.css。

设计系统：布谷 BUUGOO（https://claude.ai/artifact/Fn5TtFo5aLDYVpM9ZWujUp）
用法：python3 scripts/gen-tokens.py <tokens.json> [输出路径，默认 web/src/tokens.css]
"""
import json
import sys


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__)
        return 2
    src = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else "web/src/tokens.css"
    with open(src, encoding="utf-8") as f:
        t = json.load(f)

    lines = [
        "/* 布谷 BUUGOO — 由设计系统 tokens.json 生成（https://claude.ai/artifact/Fn5TtFo5aLDYVpM9ZWujUp）。",
        "   修改设计请改设计系统，再运行 scripts/gen-tokens.py 重新生成；不要在这里手改数值。 */",
        ":root {",
    ]
    for tok in t["color"]["tokens"]:
        v = tok["value"]
        if isinstance(v, dict):  # 多主题时取第一个主题
            v = next(iter(v.values()))
        if v.startswith("{"):
            v = "var(--%s)" % v.strip("{}")
        lines.append("  --%s: %s;" % (tok["name"], v))
    for fam, v in t["type"]["families"].items():
        lines.append("  --font-%s: %s;" % (fam, v))
    for family in ("spacing", "radius", "shadow"):
        for tok in t.get(family, {}).get("tokens", []):
            lines.append("  --%s: %s;" % (tok["name"], tok["value"]))
    lines.append("}")
    with open(out, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    print("已生成", out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
