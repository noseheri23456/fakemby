import json, os, urllib.request, urllib.error

BASE = "http://127.0.0.1:8096"


def req(method, path, token=None, data=None):
    url = BASE + path
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Emby-Token"] = token
    body = json.dumps(data).encode() if data is not None else None

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *a, **kw):
            return None

    # 本机有全局代理（HTTP_PROXY=127.0.0.1:7951 + Clash TUN），必须显式禁用
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    r = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with opener.open(r, timeout=8) as resp:
            return resp.status, resp.read().decode(errors="replace"), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace"), dict(e.headers)
    except Exception as e:
        return -1, str(e), {}


code, raw, _ = req("POST", "/emby/Users/AuthenticateByName", data={"Username": "admin", "Pw": "admin"})
login = json.loads(raw)
token = login["AccessToken"]

# Theater 实测卡住的三个 Backdrop 请求
for iid, tag in [("34ec92ccdbdd46ecb7d93a0774d79b95", "2c05ca82"),
                 ("f5b69da5d0dd4aa388e25895d4d0f7a4", "70b09d6c")]:
    code, raw, hdr = req("GET", "/emby/Items/%s/Images/Backdrop/0?tag=%s&maxWidth=1400&quality=70" % (iid, tag), token=token)
    loc = hdr.get("Location", "")
    print("item=%s 图片端点 http=%s" % (iid[:8], code))
    print("  Location: %s" % loc)
    if loc:
        # 模拟 Theater 下载（不验证证书之外，再试禁验证对照）
        import ssl
        for label, ctx in [("默认验证", None), ("忽略证书", ssl._create_unverified_context())]:
            try:
                rq = urllib.request.Request(loc, headers={"User-Agent": "Emby Theater"})
                opener2 = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                      urllib.request.HTTPSHandler(context=ctx) if ctx else urllib.request.HTTPSHandler())
                with opener2.open(rq, timeout=10) as resp:
                    data = resp.read(64 * 1024)
                    print("  [%s] http=%s 收到 %d bytes" % (label, resp.status, len(data)))
                    break
            except Exception as e:
                print("  [%s] 失败: %s" % (label, str(e)[:110]))
    # 拿条目名
    code2, raw2, _ = req("GET", "/emby/Users/%s/Items/%s" % (login["User"]["Id"], iid), token=token)
    try:
        print("  条目: %s (%s)" % (json.loads(raw2).get("Name"), json.loads(raw2).get("Type")))
    except Exception:
        pass
    print()
