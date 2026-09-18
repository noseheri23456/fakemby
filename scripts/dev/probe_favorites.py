"""验证「喜欢」的收藏 / 取消收藏链路。

用法（服务已在 8096 运行）:
    python scripts/dev/probe_favorites.py

覆盖两条真实的客户端路径：

1. 列表过滤。客户端 home/favorites.js 对人物栏调用的是 apiClient.getPeople
   （GET /emby/Persons），而不是 getItems，并带上 Filters=IsFavorite。
   列表端点忽略这个参数 = 没收藏过任何人也会长出一整栏"喜欢的人物"。

2. 取消收藏的请求形式。客户端 apiclient.js 有
       this._enablePostForDelete = this.isMinServerVersion("4.7.0.33")
   我们对外声明 4.8.0.0，所以"取消收藏 / 取消已看"实际打的是
       POST /emby/Users/{uid}/FavoriteItems/{id}/Delete
   而不是 DELETE。只注册 DELETE 的话客户端一定拿到 404 ——
   表现为"能加喜欢，不能取消喜欢"。两种形式都要能用。

注意：脚本以"基线"为准做断言（先记录当前已收藏集合，操作后要求回到基线），
不会破坏用户已有的收藏。可用 FAKEMBY_PROBE_BASE / _USER / _PW 覆盖目标。
"""

import json
import os
import urllib.error
import urllib.request

BASE = os.environ.get("FAKEMBY_PROBE_BASE", "http://127.0.0.1:8096")
USER = os.environ.get("FAKEMBY_PROBE_USER", "admin")
PW = os.environ.get("FAKEMBY_PROBE_PW", "admin")

LIST_ENDPOINTS = ("/emby/Persons", "/emby/Genres", "/emby/Studios")
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


def fav_ids(endpoint, token, uid):
    code, raw = req("GET", "%s?Filters=IsFavorite&UserId=%s" % (endpoint, uid), token=token)
    body = json.loads(raw)
    items = body.get("Items") or []
    return code, set(i.get("Id") for i in items), body.get("TotalRecordCount")


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

print("==== 1. 记录基线（用户当前真实收藏）====")
baseline = {}
for ep in LIST_ENDPOINTS:
    code, ids, total = fav_ids(ep, token, uid)
    baseline[ep] = ids
    check("%s?Filters=IsFavorite 可用" % ep, code == 200, "code=%s 已收藏 %d 条" % (code, len(ids)))

print("\n==== 2. 不带 Filters 时仍要返回全量 ====")
for ep in LIST_ENDPOINTS:
    code, raw = req("GET", "%s?UserId=%s&Limit=5" % (ep, uid), token=token)
    body = json.loads(raw)
    items = body.get("Items")
    check(
        "%s 不带过滤时非空" % ep,
        code == 200 and isinstance(items, list) and len(items) > 0,
        "code=%s total=%s" % (code, body.get("TotalRecordCount")),
    )

print("\n==== 3. 收藏 → 两种取消形式都要能回到基线 ====")
code, raw = req("GET", "/emby/Persons?UserId=%s&Limit=1" % uid, token=token)
rows = json.loads(raw).get("Items") or []
if not rows:
    check("取到一个人物用于收藏测试", False)
else:
    person = rows[0]
    pid = person["Id"]
    ep = "/emby/Persons"

    code, _ = req("POST", "/emby/Users/%s/FavoriteItems/%s" % (uid, pid), token=token)
    check("收藏人物返回 200", code in (200, 204), "code=%s" % code)

    _, ids, _ = fav_ids(ep, token, uid)
    check("收藏后出现在喜欢列表", pid in ids, "%s 共 %d 条" % (person.get("Name"), len(ids)))

    # 旧式 DELETE
    code, _ = req("DELETE", "/emby/Users/%s/FavoriteItems/%s" % (uid, pid), token=token)
    check("DELETE 取消收藏返回 200", code in (200, 204), "code=%s" % code)
    _, ids, _ = fav_ids(ep, token, uid)
    check("DELETE 后回到基线", ids == baseline[ep], "差集=%s" % (ids ^ baseline[ep]) or "无")

    # 客户端实际使用的 4.7.0.33+ 形式
    code, _ = req("POST", "/emby/Users/%s/FavoriteItems/%s" % (uid, pid), token=token)
    check("再次收藏返回 200", code in (200, 204), "code=%s" % code)

    code, _ = req("POST", "/emby/Users/%s/FavoriteItems/%s/Delete" % (uid, pid), token=token)
    check("POST .../FavoriteItems/{id}/Delete 返回 200（客户端取消收藏打的就是它）",
          code in (200, 204), "code=%s" % code)
    _, ids, _ = fav_ids(ep, token, uid)
    check("POST /Delete 后回到基线", ids == baseline[ep], "差集=%s" % (ids ^ baseline[ep]) or "无")

print("\n==== 4. 取消已看的 POST /Delete 形式同样要能用 ====")
code, raw = req("GET", "/emby/Items?IncludeItemTypes=Movie&Recursive=true&Limit=1", token=token)
movie = (json.loads(raw).get("Items") or [None])[0]
if movie:
    mid = movie["Id"]
    code, _ = req("POST", "/emby/Users/%s/PlayedItems/%s" % (uid, mid), token=token)
    check("标记已看返回 200", code in (200, 204), "code=%s" % code)
    code, raw = req("POST", "/emby/Users/%s/PlayedItems/%s/Delete" % (uid, mid), token=token)
    check("POST .../PlayedItems/{id}/Delete 返回 200", code in (200, 204), "code=%s" % code)
    if code in (200, 204):
        check("取消已看后 Played=false", json.loads(raw).get("Played") is False, raw[:120])
    # 清理：别把"已看"状态留在库里
    req("POST", "/emby/Users/%s/PlayedItems/%s/Delete" % (uid, mid), token=token)

print("\n==== 5. 常规类型的收藏过滤未被破坏 ====")
code, raw = req("GET", "/emby/Items?Filters=IsFavorite&Recursive=true&UserId=%s" % uid, token=token)
body = json.loads(raw)
items = body.get("Items") or []
bad = [it.get("Name") for it in items if not (it.get("UserData") or {}).get("IsFavorite")]
check(
    "/emby/Items?Filters=IsFavorite 返回的都是已收藏条目",
    code == 200 and not bad,
    "code=%s count=%s 未收藏却返回=%s" % (code, len(items), bad or "无"),
)

print()
if failures:
    print("❌ 失败 %d 项:" % len(failures))
    for f in failures:
        print("   - " + f)
    raise SystemExit(1)
print("✅ 全部通过")
