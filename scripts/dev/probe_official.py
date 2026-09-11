import json, os, urllib.request, urllib.error

BASE = os.environ.get("FAKEMBY_PROBE_BASE", "http://127.0.0.1:8097")


def req(method, path, token=None, data=None):
    url = BASE + path
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Emby-Token"] = token
    body = json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=10) as resp:
            return resp.status, resp.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()
    except Exception as e:
        return -1, str(e)


def show(label, code, raw, top=None):
    info = ""
    try:
        obj = json.loads(raw)
        if top:
            info = " | " + top(obj)
        elif isinstance(obj, dict):
            info = " | keys=" + str(list(obj.keys())[:8])
        elif isinstance(obj, list):
            info = " | list len=%d" % len(obj)
    except Exception:
        if raw:
            info = " | raw=%s" % raw[:80]
    print("  [%s] http=%s%s" % (label, code, info))


print("==== 1) 登录 ====")
code, raw = req("POST", "/emby/Users/AuthenticateByName",
                data={"Username": "admin", "Pw": "admin"})
login = json.loads(raw) if raw else {}
token = login.get("AccessToken")
uid = (login.get("User") or {}).get("Id")
print("  http:", code, "| token有:", bool(token), "| userId:", uid,
      "| ForcePasswordChange:", login.get("ForcePasswordChange"))

print("\n==== 2) 用户身份相关 ====")
show("Users/Me", *req("GET", "/emby/Users/Me", token=token))
show("Users/Current", *req("GET", "/emby/Users/Current", token=token))
show("Users/{id}", *req("GET", "/emby/Users/%s" % uid, token=token))
show("System/Info(auth)", *req("GET", "/emby/System/Info", token=token),
     lambda o: "Id=%s" % o.get("Id"))

print("\n==== 3) 主页库视图 ====")
show("Users/{id}/Views", *req("GET", "/emby/Users/%s/Views" % uid, token=token),
     lambda o: "TotalRecordCount=%s" % o.get("TotalRecordCount"))
show("Library/VirtualFolders", *req("GET", "/emby/Library/VirtualFolders", token=token))

print("\n==== 4) 主页各区块数据 ====")
show("Items/Latest", *req("GET", "/emby/Users/%s/Items/Latest" % uid, token=token))
show("Items/Resume", *req("GET", "/emby/Users/%s/Items/Resume" % uid, token=token))
show("Items/Counts", *req("GET", "/emby/Items/Counts", token=token))
show("DisplayPreferences", *req("GET",
     "/emby/DisplayPreferences/usersettings?userId=" + uid + "&client=Emby%20Web", token=token))

print("\n==== 5) 点击库后(Items?ParentId=某库) ====")
# 取第一个视图 id
_, raw = req("GET", "/emby/Users/%s/Views" % uid, token=token)
try:
    first = json.loads(raw)["Items"][0]["Id"]
    show("Items?ParentId=首库", *req("GET",
         "/emby/Users/%s/Items?ParentId=%s" % (uid, first), token=token))
except Exception as e:
    print("  (取不到视图id)", e)

print("\n==== 6) 官方客户端可能请求但本项目未必实现的端点(404=缺失) ====")
show("Channels", *req("GET", "/emby/Channels", token=token))
show("QuickConnect/Enabled", *req("GET", "/emby/QuickConnect/Enabled", token=token))
show("Items/Filters", *req("GET", "/emby/Items/Filters", token=token))
show("UserLibrary/ReviewedItems", *req("GET", "/emby/UserLibrary/ReviewedItems", token=token))
