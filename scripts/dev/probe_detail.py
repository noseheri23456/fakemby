import json, os, urllib.request, urllib.error

BASE = os.environ.get("FAKEMBY_PROBE_BASE", "http://127.0.0.1:8096")


def req(method, path, token=None, data=None):
    url = BASE + path
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Emby-Token"] = token
    body = json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=8) as resp:
            return resp.status, resp.read().decode(errors="replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")
    except Exception as e:
        return -1, str(e)


code, raw = req("POST", "/emby/Users/AuthenticateByName",
                data={"Username": "admin", "Pw": "admin"})
login = json.loads(raw)
token = login["AccessToken"]
uid = login["User"]["Id"]

print("==== Users/Me 完整响应 ====")
code, raw = req("GET", "/emby/Users/Me", token=token)
print(json.dumps(json.loads(raw), indent=1, ensure_ascii=False))

print("\n==== Views 第一个库完整响应 ====")
code, raw = req("GET", "/emby/Users/%s/Views" % uid, token=token)
views = json.loads(raw)
if views.get("Items"):
    print(json.dumps(views["Items"][0], indent=1, ensure_ascii=False)[:2500])

print("\n==== Latest 首条完整响应 ====")
code, raw = req("GET", "/emby/Users/%s/Items/Latest" % uid, token=token)
latest = json.loads(raw)
if isinstance(latest, list) and latest:
    print(json.dumps(latest[0], indent=1, ensure_ascii=False)[:2500])
