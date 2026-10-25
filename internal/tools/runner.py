# 包装用户代码：运行结束后收集代码生成的图片（保存到工作目录的图片文件，或没有保存的 matplotlib 图表），
# 以 BOUNDARY 分隔附在标准输出末尾交给布谷。由 Go 填入 BOUNDARY 与 CODE。
import base64 as _b64, json as _json, linecache as _lc, os as _os, sys as _sys, time as _time, traceback as _tb, warnings as _warn

_BOUNDARY = "{{BOUNDARY}}"
_SRC = _b64.b64decode("{{CODE}}").decode("utf-8")
_EXTS = {".png", ".jpg", ".jpeg", ".gif", ".webp"}
_MAX_IMAGES, _MAX_EACH, _MAX_TOTAL = {{MAX_IMAGES}}, {{MAX_EACH}}, {{MAX_TOTAL}}


def _setup():
    wd = _os.getcwd()
    # matplotlib：不弹窗口；优先用中文字体；沿用预先建好的字体缓存，避免每次运行都扫描字体。
    rc = _os.path.join(wd, ".buugoo-matplotlibrc")
    with open(rc, "w") as f:
        f.write("backend: Agg\naxes.unicode_minus: False\n"
                "font.sans-serif: Noto Sans CJK SC, Noto Sans SC, Source Han Sans SC, WenQuanYi Micro Hei, "
                "WenQuanYi Zen Hei, SimHei, Microsoft YaHei, PingFang SC, DejaVu Sans\n")
    _os.environ["MATPLOTLIBRC"] = rc
    _os.environ.setdefault("MPLCONFIGDIR", _os.path.join(wd, ".matplotlib"))
    cache = _os.environ.pop("BUUGOO_MPL_CACHE", "")
    if cache and _os.path.isdir(cache):
        dst = _os.environ["MPLCONFIGDIR"]
        try:
            _os.makedirs(dst, exist_ok=True)
            for name in _os.listdir(cache):
                if name.startswith("fontlist") and not _os.path.exists(_os.path.join(dst, name)):
                    with open(_os.path.join(cache, name), "rb") as s, open(_os.path.join(dst, name), "wb") as d:
                        d.write(s.read())
        except OSError:
            pass
    # 字体回退的提示对结果没有影响，不让它混进错误输出。
    import logging
    logging.getLogger("matplotlib.font_manager").setLevel(logging.ERROR)
    _warn.filterwarnings("ignore", message=".*non-interactive.*")
    _warn.filterwarnings("ignore", message=".*Glyph .* missing from.*")
    return wd


def _files(wd, since):
    found = []
    for root, dirs, names in _os.walk(wd):
        dirs[:] = [d for d in dirs if not d.startswith(".")] if root == wd else []
        for n in names:
            p = _os.path.join(root, n)
            if n.startswith(".") or _os.path.splitext(n)[1].lower() not in _EXTS or _os.path.islink(p):
                continue
            try:
                st = _os.stat(p)
            except OSError:
                continue
            if st.st_mtime >= since - 1:
                found.append((st.st_mtime, _os.path.relpath(p, wd), p))
    found.sort()
    out = []
    for _, name, p in found:
        try:
            with open(p, "rb") as f:
                out.append((name, f.read(_MAX_EACH + 1)))
        except OSError:
            pass
    return out


def _figures():
    plt = _sys.modules.get("matplotlib.pyplot")
    if plt is None:
        return []
    import io
    out = []
    for num in plt.get_fignums():
        buf = io.BytesIO()
        try:
            plt.figure(num).savefig(buf, format="png", dpi=120, bbox_inches="tight")
        except Exception:
            continue
        out.append(("figure_%d.png" % num, buf.getvalue()))
    return out


def _collect(fd, wd, since):
    try:
        imgs = _files(wd, since)
        if not imgs:
            imgs = _figures()
    except Exception:
        imgs = []
    items, total, skipped = [], 0, 0
    for name, data in imgs:
        if len(items) >= _MAX_IMAGES or len(data) > _MAX_EACH or total + len(data) > _MAX_TOTAL:
            skipped += 1
            continue
        total += len(data)
        items.append({"name": name, "data": _b64.b64encode(data).decode()})
    if not items and not skipped:
        return
    payload = _json.dumps({"images": items, "skipped": skipped}).encode()
    for s in (_sys.stdout, _sys.stderr):
        try:
            s.flush()
        except Exception:
            pass
    try:
        _os.write(fd, b"\n" + _BOUNDARY.encode() + b"\n")
        view = memoryview(payload)
        while view:
            view = view[_os.write(fd, view):]
    except OSError:
        pass


def _main():
    wd = _setup()
    fd = _os.dup(1)
    since = _time.time()
    _lc.cache["<code>"] = (len(_SRC), None, _SRC.splitlines(True), "<code>")
    g = {"__name__": "__main__", "__builtins__": __builtins__}
    status = 0
    try:
        exec(compile(_SRC, "<code>", "exec"), g)
    except SystemExit as e:
        status = e.code
    except BaseException as e:
        # 去掉包装代码自己的那一层，让错误信息只指向用户代码。
        _tb.print_exception(type(e), e, e.__traceback__.tb_next)
        status = 1
    _collect(fd, wd, since)
    _sys.exit(status)


_main()
