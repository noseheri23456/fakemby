import json, os, urllib.request, urllib.error

BASE = os.environ.get("FAKEMBY_PROBE_BASE", "http://127.0.0.1:8096")


def req(method, path, token=None, data=None, no_redirect=False):
    url = BASE + path
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Emby-Token"] = token
    body = json.dumps(data).encode() if data is not None else None

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *a, **kw):
            return None

    handlers = [NoRedirect()] if no_redirect else []
    opener = urllib.request.build_opener(*handlers)
    r = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with opener.open(r, timeout=8) as resp:
            return resp.status, resp.read().decode(errors="replace"), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace"), dict(e.headers)
    except Exception as e:
        return -1, str(e), {}


def show(label, code, raw, extra=""):
    info = ""
    try:
        obj = json.loads(raw)
        if isinstance(obj, dict):
            info = " | keys=" + str(list(obj.keys())[:10])
        elif isinstance(obj, list):
            info = " | list len=%d" % len(obj)
    except Exception:
        if raw:
            info = " | raw=%s" % raw[:70]
    print("  [%s] http=%s%s%s" % (label, code, info, extra))


code, raw, _ = req("POST", "/emby/Users/AuthenticateByName",
                   data={"Username": "admin", "Pw": "admin"})
login = json.loads(raw) if raw else {}
token = login.get("AccessToken")
uid = (login.get("User") or {}).get("Id")
print("login http=%s token=%s" % (code, bool(token)))

print("\n==== 转圈高嫌疑端点 ====")
show("GET System/Configuration", *req("GET", "/emby/System/Configuration", token=token)[:2])
show("GET System/Configuration/public", *req("GET", "/emby/System/Configuration/public", token=None)[:2])
show("GET Branding/Configuration", *req("GET", "/emby/Branding/Configuration", token=token)[:2])
show("GET Sessions (当前会话)", *req("GET", "/emby/Sessions", token=token)[:2])
show("GET Plugins", *req("GET", "/emby/Plugins", token=token)[:2])
show("GET System/ActivityLog/Entries", *req("GET",
     "/emby/System/ActivityLog/Entries?startIndex=0&limit=10", token=token)[:2])
show("GET LiveTv/Tuners", *req("GET", "/emby/LiveTv/Tuners", token=token)[:2])
show("GET System/Ping", *req("GET", "/emby/System/Ping", token=None)[:2])
show("POST Sessions/Capabilities/Full", *req("POST", "/emby/Sessions/Capabilities/Full",
     token=token, data={"PlayableMediaTypes": ["Video"], "SupportedCommands": []})[:2])
show("GET Auth/Keys", *req("GET", "/emby/Auth/Keys", token=token)[:2])

print("\n==== WebSocket ====")
# HTTP 探测 embysocket（不发真实 Upgrade，只看是否 404 还是 400/200——404=路由缺失）
show("GET /embysocket (探测)", *req("GET", "/embysocket?api_key=" + str(token), token=token)[:2])
show("GET /emby/embysocket (探测)", *req("GET", "/emby/embysocket?api_key=" + str(token), token=token)[:2])

print("\n==== 图片链路（redirect 模式 302 目标可达性） ====")
_, raw, _ = req("GET", "/emby/Users/%s/Items/Latest" % uid, token=token)
try:
    first = json.loads(raw)[0]
    iid = first["Id"]
    print("  首条 Latest: %s (%s)" % (first.get("Name"), iid))
    code, raw, hdr = req("GET",
        "/emby/Items/%s/Images/Primary?maxWidth=400" % iid, token=token, no_redirect=True)
    loc = hdr.get("Location", "")
    print("  图片端点 http=%s | 302 Location=%s" % (code, loc[:100]))
    if loc:
        # 直接探测外部目标（不跟随重定向链之外的重定向，只看连通性）
        try:
            rq = urllib.request.Request(loc, headers={"User-Agent": "Emby"})
            with urllib.request.urlopen(rq, timeout=8) as resp:
                data = resp.read(64 * 1024)
                print("  外部图源 http=%s 收到 %d bytes" % (resp.status, len(data)))
        except Exception as e:
            print("  外部图源不可达: %s" % str(e)[:120])
except Exception as e:
    print("  (Latest 解析失败)", e)
