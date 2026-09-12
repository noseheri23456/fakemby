#!/usr/bin/env python3
"""Emby 官方客户端响应字段安全审计（M3-1）。

背景
----
官方客户端（Emby Theater 等）对响应里的数组/对象字段存在大量**裸调用**：

    user.Configuration.LatestItemsExcludes.includes(item.Id)
    mediaSource.RequiredHttpHeaders.length
    item.MediaSources.filter(...)

JS 里 `undefined.length` / `null.includes(...)` 都是 TypeError。一旦触发，
渲染 Promise 链就断——服务端日志只能看到「请求流到此为止」，看不到原因
（表现为首页无限转圈 / 详情页 "Content no longer available"）。

因此**凡被客户端裸调的字段，服务端必须恒返回 [] / {}，绝不能缺失或为 null**。
Go 里两个坑都会造成字段缺失：
  1. nil 切片 / nil map → JSON 序列化成 null（同样会崩）
  2. 带了 `omitempty` → 空切片/空 map 被整个省略（等于 undefined）

本脚本做两件事：
  1. 扫描客户端源码，提取所有「裸调字段」及其调用形态；
  2. 与本项目 DTO 的 json tag 交叉比对，标出风险等级。

用法
----
    python scripts/dev/audit_client_fields.py                 # 默认扫 Emby Theater
    python scripts/dev/audit_client_fields.py --client-dir X  # 指定客户端 www 目录
    python scripts/dev/audit_client_fields.py --top 40        # 只显示前 N 个字段

退出码：发现 HIGH 风险返回 1，否则 0（可用于 CI 门禁）。
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from collections import Counter

# 客户端源码里出现这些形态，说明该字段被当成数组/字符串/对象直接用了
ARRAY_METHODS = (
    "length",
    "includes",
    "filter",
    "map",
    "forEach",
    "indexOf",
    "some",
    "every",
    "reduce",
    "sort",
    "slice",
    "join",
    "split",
    "concat",
    "find",
    "findIndex",
    "push",
    "keys",
)

# 匹配 `.SomeField .method(` 或 `.SomeField .length`
# 字段名按 Emby DTO 惯例是 PascalCase
FIELD_CALL_RE = re.compile(
    r"\.([A-Z][A-Za-z0-9_]*)\s*\.\s*(" + "|".join(ARRAY_METHODS) + r")\b"
)

# 客户端里明确作为响应对象使用的变量名（用于降低误报）
RESPONSE_HINTS = (
    "item",
    "items",
    "user",
    "result",
    "data",
    "mediaSource",
    "mediaSources",
    "source",
    "session",
    "serverInfo",
    "systemInfo",
    "userData",
    "policy",
    "configuration",
    "chapter",
    "stream",
    "view",
    "library",
    "resp",
    "response",
    "info",
)

# 明显不是 DTO 字段的误报（JS 内置 / 通用库）
NOISE_FIELDS = {
    "Object",
    "Array",
    "String",
    "Number",
    "JSON",
    "Math",
    "Date",
    "Promise",
    "Error",
    "Event",
    "Location",
    "History",
    "Window",
    "Document",
    "Options",
    "Arguments",
    "Arguments",
    "Prototype",
    "Constructor",
    "Locale",
    "DateTime",
    "Time",
    "Url",
    "URL",
    "Path",
    "Paths",
    "Files",
    "Language",
    "Languages",
}


def iter_client_js(client_dir: str):
    for root, dirs, files in os.walk(client_dir):
        dirs[:] = [d for d in dirs if d not in ("bower_components", "node_modules")]
        for fn in files:
            if fn.endswith(".js"):
                yield os.path.join(root, fn)


def scan_client(client_dir: str) -> tuple[Counter, Counter]:
    """返回 (裸调次数, 有兜底的次数)。

    只有「裸调」才是风险：客户端写了 `x.F && x.F.length` 时短路保护生效，
    undefined/null 都不会抛异常，这类字段不必强制服务端输出。
    """
    hits: Counter = Counter()
    guarded_hits: Counter = Counter()
    for path in iter_client_js(client_dir):
        try:
            with open(path, encoding="utf-8-sig", errors="replace") as f:
                src = f.read()
        except OSError:
            continue
        for m in FIELD_CALL_RE.finditer(src):
            field = m.group(1)
            if field in NOISE_FIELDS:
                continue
            # 取左侧 40 字符判断是否贴着响应对象名，粗略降噪
            left = src[max(0, m.start() - 40) : m.start()]
            if not any(h in left.lower() for h in RESPONSE_HINTS):
                continue
            # 短路兜底：`item.Field && item.Field.length` —— undefined 也安全。
            # 形如 `.Field &&` 出现在本次调用左侧即视为已防御。
            guarded = re.search(
                rf"{re.escape(field)}\s*(?:&&|\|\||\?\?)", src[max(0, m.start() - 60) : m.start()]
            )
            if guarded:
                guarded_hits[field] += 1
                continue
            hits[field] += 1
    return hits, guarded_hits


# ---- DTO 侧：解析 internal/types/*.go 的 json tag ----

GO_FIELD_RE = re.compile(
    r"^\s*([A-Z]\w*)\s+([^`\s]+(?:\s*\[\]\s*[^`\s]+)?)\s+`json:\"([^\"]+)\"`",
    re.MULTILINE,
)


def scan_dto(search_dirs: list[str]) -> dict[str, dict]:
    """返回 {json字段名: {type, omitempty, file}}

    注意：DTO 不只定义在 internal/types（如 UserConfig 在 internal/emby/auth.go），
    因此扫描目录可配置，缺失包会导致已修字段被误报成「DTO 中无此字段」。
    """
    dto: dict[str, dict] = {}
    for d in search_dirs:
        if not os.path.isdir(d):
            continue
        for root, _dirs, files in os.walk(d):
            for fn in sorted(files):
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                path = os.path.join(root, fn)
                try:
                    with open(path, encoding="utf-8") as f:
                        src = f.read()
                except OSError:
                    continue
                for m in GO_FIELD_RE.finditer(src):
                    go_type = m.group(2).replace(" ", "")
                    tag = m.group(3)
                    name = tag.split(",")[0]
                    if not name or name == "-":
                        continue
                    dto.setdefault(
                        name,
                        {
                            "type": go_type,
                            "omitempty": ",omitempty" in tag,
                            "file": os.path.relpath(path, d),
                        },
                    )
    return dto


def risk_of(field: str, dto: dict[str, dict]) -> tuple[str, str]:
    """返回 (风险等级, 说明)"""
    info = dto.get(field)
    if info is None:
        return "MEDIUM", "DTO 中无此字段（客户端拿到 undefined）"
    t = info["type"]
    is_container = t.startswith("[]") or t.startswith("map[") or t.startswith("*")
    if not is_container:
        return "LOW", f"DTO 有此字段（{t}），值类型不会为 null"
    if t.startswith("*") and info["omitempty"]:
        return "HIGH", f"指针类型 {t} 且 omitempty：零值被省略 → undefined"
    if is_container and info["omitempty"]:
        return "HIGH", f"容器类型 {t} 且 omitempty：空值被省略 → undefined（裸调必崩）"
    if is_container:
        return "MEDIUM", f"容器类型 {t} 无 omitempty，但需确认构造点初始化了空值（nil 会序列化成 null）"
    return "LOW", "OK"


def main() -> int:
    repo = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    # 官方客户端安装位置不固定（本机在 F: 盘），依次探测常见路径
    candidates = []
    env_dir = os.environ.get("FAKEMBY_CLIENT_DIR")
    if env_dir:
        candidates.append(env_dir)
    for pf in (
        os.environ.get("PROGRAMFILES"),
        os.environ.get("PROGRAMFILES(X86)"),
        "C:\\Program Files",
        "D:\\Program Files",
        "E:\\Program Files",
        "F:\\Program Files",
    ):
        if pf:
            candidates.append(os.path.join(pf, "Emby Theater", "electronapp", "www"))
    default_client = next((c for c in candidates if os.path.isdir(c)), candidates[-1])

    ap = argparse.ArgumentParser(description="Emby 客户端响应字段安全审计")
    ap.add_argument("--client-dir", default=default_client, help="客户端 www 目录")
    ap.add_argument("--repo", default=repo, help="fakemby 仓库根目录")
    ap.add_argument("--top", type=int, default=30, help="显示前 N 个字段")
    ap.add_argument(
        "--show-low", action="store_true", help="同时显示 LOW 风险字段（默认隐藏）"
    )
    args = ap.parse_args()

    if not os.path.isdir(args.client_dir):
        print(f"[!] 客户端源码目录不存在：{args.client_dir}", file=sys.stderr)
        print("    用 --client-dir 指定，或先安装 Emby Theater。", file=sys.stderr)
        return 2

    hits, guarded = scan_client(args.client_dir)
    dto_dirs = [
        os.path.join(args.repo, "internal", "types"),
        os.path.join(args.repo, "internal", "api"),
    ]
    dto = scan_dto(dto_dirs)

    def guarded_note(field: str) -> str:
        n = guarded.get(field, 0)
        return f"（另有 {n} 处带短路兜底）" if n else ""

    print(f"客户端源码：{args.client_dir}")
    print(f"DTO 扫描  ：internal/types, internal/api（{len(dto)} 个 json 字段）")
    print(f"裸调字段  ：{len(hits)} 个；另有 {len(guarded)} 个字段客户端自带短路兜底\n")

    rows = []
    for field, count in hits.most_common():
        level, why = risk_of(field, dto)
        rows.append((level, count, field, why))

    order = {"HIGH": 0, "MEDIUM": 1, "LOW": 2}
    rows.sort(key=lambda r: (order[r[0]], -r[1]))

    high = [r for r in rows if r[0] == "HIGH"]
    med = [r for r in rows if r[0] == "MEDIUM"]
    low = [r for r in rows if r[0] == "LOW"]

    def dump(title: str, group: list):
        if not group:
            return
        print(f"===== {title}（{len(group)}）=====")
        print(f"{'字段':<34}{'裸调':>5}  说明")
        for level, count, field, why in group[: args.top]:
            print(f"{field:<34}{count:>5}  {why}")
        print()

    dump("HIGH · 空值会崩，必须修", high)
    dump("MEDIUM · 缺失字段或需确认初始化", med)
    if args.show_low:
        dump("LOW · 值类型，安全", low)

    print(f"汇总：HIGH {len(high)} / MEDIUM {len(med)} / LOW {len(low)}")
    if high:
        print("\n修复方向：去掉 omitempty（容器字段恒输出 [] / {}），并在构造点初始化空值。")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
