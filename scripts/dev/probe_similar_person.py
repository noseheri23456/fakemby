"""验证「更多类似的直播电视」误显 + 演员详情页空白 两项修复。

用法（服务已在 8096 运行）:
    python scripts/dev/probe_similar_person.py

断言:
  1. /emby/Items/{movie}/Similar?IncludeItemTypes=Program  -> Items 为空
     （客户端 itemhelper.supportsSimilarItemsOnLiveTV 对 Movie/Series 恒 true，
      靠这个请求返回空来隐藏「更多类似的直播电视」栏目）
  2. /emby/Items/{movie}/Similar 不带类型                   -> Items 非空
  3. /emby/Items/{person}                                   -> ServerId 非空
     （客户端用 item.ServerId 反查 apiClient，缺失会 TypeError 整页空白）
"""

import json
import os
import urllib.error
import urllib.request

BASE = os.environ.get("FAKEMBY_PROBE_BASE", "http://127.0.0.1:8096")
USER = os.environ.get("FAKEMBY_PROBE_USER", "admin")
PW = os.environ.get("FAKEMBY_PROBE_PW", "admin")

failures = []


def req(method, path, token=None, data=None):
    url = BASE + path
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Emby-Token"] = token
    body = json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=10) as resp:
            return resp.status, resp.read().decode(errors="replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")
    except Exception as e:  # noqa: BLE001
        return -1, str(e)


def check(name, ok, detail=""):
    print(("  [OK]   " if ok else "  [FAIL] ") + name + (("  " + detail) if detail else ""))
    if not ok:
        failures.append(name)


code, raw = req("POST", "/emby/Users/AuthenticateByName", data={"Username": USER, "Pw": PW})
if code != 200:
    raise SystemExit("登录失败: %s %s" % (code, raw[:200]))
login = json.loads(raw)
token = login["AccessToken"]
uid = login["User"]["Id"]
print("登录成功 user=%s\n" % uid)

# 找一个电影和一个剧集
code, raw = req("GET", "/emby/Items?IncludeItemTypes=Movie&Recursive=true&Limit=1", token=token)
movie = (json.loads(raw).get("Items") or [None])[0]
code, raw = req("GET", "/emby/Items?IncludeItemTypes=Series&Recursive=true&Limit=1", token=token)
series = (json.loads(raw).get("Items") or [None])[0]

if not movie or not series:
    raise SystemExit("库里没有 Movie/Series，先导入数据")

print("样例: Movie=%s Series=%s\n" % (movie["Name"], series["Name"]))

print("==== 1. Similar + IncludeItemTypes=Program 必须为空 ====")
for label, item in (("Movie", movie), ("Series", series)):
    code, raw = req(
        "GET",
        "/emby/Items/%s/Similar?IncludeItemTypes=Program&Limit=12&UserId=%s" % (item["Id"], uid),
        token=token,
    )
    body = json.loads(raw)
    items = body.get("Items")
    total = body.get("TotalRecordCount")
    check(
        "%s Similar(Program) 返回 200 且 Items 为空数组" % label,
        code == 200 and isinstance(items, list) and len(items) == 0,
        "code=%s total=%s" % (code, total),
    )

print("\n==== 2. Similar 不带类型仍需返回相似项 ====")
for label, item in (("Movie", movie), ("Series", series)):
    code, raw = req(
        "GET", "/emby/Items/%s/Similar?Limit=12&UserId=%s" % (item["Id"], uid), token=token
    )
    body = json.loads(raw)
    items = body.get("Items")
    check(
        "%s Similar(无过滤) Items 非空" % label,
        code == 200 and isinstance(items, list) and len(items) > 0,
        "code=%s count=%s" % (code, len(items) if isinstance(items, list) else items),
    )

print("\n==== 3. 人物详情 DTO 必须带 ServerId ====")
code, raw = req(
    "GET", "/emby/Persons?Limit=5&Recursive=true", token=token
)
persons = (json.loads(raw).get("Items") or [])
if not persons:
    code, raw = req("GET", "/emby/Items?IncludeItemTypes=Person&Recursive=true&Limit=5", token=token)
    persons = json.loads(raw).get("Items") or []

if not persons:
    check("找到至少一个 Person", False)
else:
    ok = True
    for p in persons[:3]:
        code, raw = req("GET", "/emby/Items/%s?UserId=%s" % (p["Id"], uid), token=token)
        body = json.loads(raw)
        sid = body.get("ServerId")
        good = code == 200 and bool(sid)
        ok = ok and good
        check(
            "Person %s 详情 200 且 ServerId 非空" % p.get("Name", p["Id"]),
            good,
            "code=%s ServerId=%r Type=%s" % (code, sid, body.get("Type")),
        )

    # 人物参与的条目列表（详情页下半部分）
    p = persons[0]
    code, raw = req(
        "GET",
        "/emby/Items?PersonIds=%s&Recursive=true&IncludeItemTypes=Movie,Series&Limit=10&UserId=%s"
        % (p["Id"], uid),
        token=token,
    )
    body = json.loads(raw)
    items = body.get("Items")
    check(
        "Person 关联条目查询可用",
        code == 200 and isinstance(items, list),
        "code=%s count=%s" % (code, len(items) if isinstance(items, list) else items),
    )

print("\n==== 4. 虚拟条目（Genre/Studio）同样带 ServerId ====")
# 注意：不能用 /emby/Items?IncludeItemTypes=Genre 取样例 —— 虚拟条目没有 library_id，
# 会被访问作用域过滤掉（按设计如此）。列表走专属端点。
for path, t in (("/emby/Genres?Limit=1", "Genre"), ("/emby/Studios?Limit=1", "Studio")):
    code, raw = req("GET", path, token=token)
    rows = json.loads(raw).get("Items") or []
    if not rows:
        check("%s 列表返回条目" % t, False, "code=%s" % code)
        continue
    code, raw = req("GET", "/emby/Items/%s?UserId=%s" % (rows[0]["Id"], uid), token=token)
    body = json.loads(raw)
    check(
        "%s %s 详情 200 且 ServerId 非空" % (t, rows[0].get("Name")),
        code == 200 and bool(body.get("ServerId")),
        "code=%s ServerId=%r" % (code, body.get("ServerId")),
    )

# 列表条目本身也要带 ServerId：客户端点分类卡片时用的是列表里的对象。
for path in ("/emby/Genres?Limit=3", "/emby/Studios?Limit=3", "/emby/Persons?Limit=3"):
    code, raw = req("GET", path, token=token)
    rows = json.loads(raw).get("Items") or []
    missing = [r.get("Name") for r in rows if not r.get("ServerId")]
    check(
        "%s 列表条目全部带 ServerId" % path.split("?")[0],
        code == 200 and rows and not missing,
        "code=%s count=%s 缺失=%s" % (code, len(rows), missing or "无"),
    )

print()
if failures:
    print("❌ 失败 %d 项:" % len(failures))
    for f in failures:
        print("   - " + f)
    raise SystemExit(1)
print("✅ 全部通过")
