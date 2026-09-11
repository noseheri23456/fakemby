import json, os, urllib.request, urllib.error

BASE = os.environ.get("FAKEMBY_PROBE_BASE", "http://127.0.0.1:8096")


def req(method, path, token=None, data=None):
    url = BASE + path
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Emby-Token"] = token
    body = json.dumps(data).encode() if data is not None else None

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *a, **kw):
            return None

    opener = urllib.request.build_opener(NoRedirect())
    r = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with opener.open(r, timeout=8) as resp:
            return resp.status, resp.read().decode(errors="replace"), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace"), dict(e.headers)
    except Exception as e:
        return -1, str(e), {}


code, raw, _ = req("POST", "/emby/Users/AuthenticateByName",
                   data={"Username": "admin", "Pw": "admin"})
login = json.loads(raw)
token = login["AccessToken"]
uid = login["User"]["Id"]

# 找所有带 Primary 图的条目，逐个测图片端点 302 目标可达性
code, raw, _ = req("GET", "/emby/Users/%s/Items/Latest" % uid, token=token)
items = json.loads(raw)
print("Latest 返回 %d 条" % len(items))
checked = 0
for it in items:
    iid = it["Id"]
    tags = it.get("ImageTags") or {}
    if "Primary" not in tags:
        print("  %s (%s): 无 Primary 图" % (it.get("Name"), it.get("Type")))
        continue
    code, raw, hdr = req("GET", "/emby/Items/%s/Images/Primary?maxWidth=400" % iid, token=token)
    loc = hdr.get("Location", "")
    print("  %s (%s): 图片端点 http=%s Location=%s" % (it.get("Name"), it.get("Type"), code, loc[:90]))
    if loc:
        try:
            rq = urllib.request.Request(loc, headers={"User-Agent": "Emby Theater"})
            with urllib.request.urlopen(rq, timeout=8) as resp:
                data = resp.read(64 * 1024)
                print("    外部图源 http=%s 收到 %d bytes" % (resp.status, len(data)))
        except Exception as e:
            print("    外部图源不可达: %s" % str(e)[:130])
    checked += 1
    if checked >= 5:
        break
