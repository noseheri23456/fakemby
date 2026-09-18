"""验证「喜欢」页的收藏过滤。

用法（服务已在 8096 运行）:
    python scripts/dev/probe_favorites.py

背景：客户端 home/favorites.js 对「人物」段调用的是 apiClient.getPeople
（GET /emby/Persons），而不是 getItems，并带上 Filters=IsFavorite。
列表端点忽略这个参数 = 没收藏过任何人也会长出一整栏"喜欢的人物"。

断言:
  1. 当前用户没有收藏任何人物时，/emby/Persons?Filters=IsFavorite 必须为空
  2. 收藏某个人物后，它出现在结果里；取消收藏后消失
  3. 不带 Filters 时仍返回全量（修复不能把正常列表弄空）
  4. /emby/Genres、/emby/Studios 同理
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

ENDPOINTS = ("/emby/Persons", "/emby/Genres", "/emby/Studios")

print("==== 1. 未收藏时 ?Filters=IsFavorite 必须为空 ====")
for ep in ENDPOINTS:
    code, raw = req("GET", "%s?Filters=IsFavorite&UserId=%s" % (ep, uid), token=token)
    body = json.loads(raw)
    items = body.get("Items")
    check(
        "%s?Filters=IsFavorite 返回空集" % ep,
        code == 200 and isinstance(items, list) and len(items) == 0,
        "code=%s count=%s" % (code, len(items) if isinstance(items, list) else items),
    )

print("\n==== 2. 不带 Filters 仍要返回全量 ====")
for ep in ENDPOINTS:
    code, raw = req("GET", "%s?UserId=%s&Limit=5" % (ep, uid), token=token)
    body = json.loads(raw)
    items = body.get("Items")
    check(
        "%s 不带过滤时非空" % ep,
        code == 200 and isinstance(items, list) and len(items) > 0,
        "code=%s total=%s" % (code, body.get("TotalRecordCount")),
    )

print("\n==== 3. 收藏人物后应出现在结果里，取消后应消失 ====")
code, raw = req("GET", "/emby/Persons?UserId=%s&Limit=1" % uid, token=token)
rows = json.loads(raw).get("Items") or []
if not rows:
    check("取到一个人物用于收藏测试", False)
else:
    person = rows[0]
    pid = person["Id"]
    base = "%s?Filters=IsFavorite&UserId=%s" % ("/emby/Persons", uid)

    code, _ = req("POST", "/emby/Users/%s/FavoriteItems/%s" % (uid, pid), token=token)
    check("收藏人物返回 200", code in (200, 204), "code=%s" % code)

    code, raw = req("GET", base, token=token)
    items = json.loads(raw).get("Items") or []
    ids = [i.get("Id") for i in items]
    check(
        "收藏后 %s 出现在喜欢列表" % person.get("Name"),
        pid in ids,
        "code=%s count=%s" % (code, len(items)),
    )

    code, _ = req("DELETE", "/emby/Users/%s/FavoriteItems/%s" % (uid, pid), token=token)
    check("取消收藏返回 200", code in (200, 204), "code=%s" % code)

    code, raw = req("GET", base, token=token)
    items = json.loads(raw).get("Items") or []
    check(
        "取消后喜欢列表回到空",
        len(items) == 0,
        "code=%s count=%s" % (code, len(items)),
    )

print("\n==== 4. 电影/剧集等常规类型的收藏过滤未被破坏 ====")
# 注意：库里本来就可能真有收藏（例如用户自己点过♥的电影），所以这里不断言"必须为空"，
# 而是断言"返回的每一条都确实是已收藏的"。
code, raw = req("GET", "/emby/Items?Filters=IsFavorite&Recursive=true&UserId=%s" % uid, token=token)
body = json.loads(raw)
items = body.get("Items") or []
bad = []
for it in items:
    ud = it.get("UserData") or {}
    if not ud.get("IsFavorite"):
        bad.append(it.get("Name"))
check(
    "/emby/Items?Filters=IsFavorite 返回的都是已收藏条目",
    code == 200 and isinstance(items, list) and not bad,
    "code=%s count=%s 未收藏却返回=%s" % (code, len(items), bad or "无"),
)

print()
if failures:
    print("❌ 失败 %d 项:" % len(failures))
    for f in failures:
        print("   - " + f)
    raise SystemExit(1)
print("✅ 全部通过")
