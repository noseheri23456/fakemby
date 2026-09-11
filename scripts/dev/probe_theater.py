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
print("login http=%s" % code)

# Emby Theater 3.0.20 实测请求过的全部端点（来自 8096 日志）
endpoints = [
    ("GET", "/emby/Users/Me", True),
    ("GET", "/emby/Users/%s" % uid, True),
    ("GET", "/emby/Users/%s/Views" % uid, True),
    ("GET", "/emby/System/Info", True),
    ("GET", "/emby/System/Info/Public", False),
    ("GET", "/emby/System/Configuration", True),
    ("GET", "/emby/System/Configuration/public", False),
    ("GET", "/emby/Branding/Configuration", True),
    ("GET", "/emby/Sessions", True),
    ("GET", "/emby/Plugins", True),
    ("GET", "/emby/ScheduledTasks", True),
    ("GET", "/emby/System/ActivityLog/Entries?startIndex=0&limit=10", True),
    ("GET", "/emby/System/ActivityLog/Entries?StartIndex=0&Limit=10&MinDate=2026-09-04T19:38:56.458Z&hasUserId=true", True),
    ("GET", "/emby/LiveTv/Tuners", True),
    ("GET", "/emby/LiveTv/Recordings?UserId=%s&IsInProgress=true" % uid, True),
    ("GET", "/emby/System/Ping", False),
    ("GET", "/emby/Artists?SortBy=SortName&Filters=IsFavorite&Recursive=true&Limit=12&userId=%s" % uid, True),
    ("GET", "/emby/Persons", True),
    ("GET", "/emby/Auth/Keys", True),
    ("GET", "/emby/web/configurationpages?PageType=PluginConfiguration&EnableInMainMenu=true", True),
    ("GET", "/emby/Users/%s/Items/Latest" % uid, True),
    ("GET", "/emby/Users/%s/Items/Resume" % uid, True),
    ("GET", "/emby/Users/%s/Items" % uid, True),
    ("GET", "/emby/DisplayPreferences/usersettings?userId=%s&client=emby" % uid, True),
    ("GET", "/emby/Library/VirtualFolders", True),
    ("GET", "/emby/Items/Counts", True),
    ("GET", "/emby/Channels", True),          # 仍 404（M3-4 范围，可降级）
    ("GET", "/emby/QuickConnect/Enabled", True),  # 仍 404（可降级）
    ("GET", "/emby/Items/Filters", True),     # 仍 404（可降级）
]
bad = 0
for method, path, need_auth in endpoints:
    t = token if need_auth else None
    code, raw = req(method, path, token=t)
    mark = "OK " if code == 200 else ("!! " if code == 404 else "?? ")
    if code != 200:
        bad += 1
    print("  %s http=%-4s %s" % (mark, code, path.split("?")[0]))

# WebSocket 路径（HTTP 探测：应不再是 404，无 Upgrade 头时 400 = 路由存在）
for p in ["/embysocket?api_key=x", "/emby/embysocket?api_key=x"]:
    code, raw = req("GET", p)
    print("  %s http=%-4s %s (400=路由存在但非WS握手)" % ("OK " if code in (200, 400) else "!! ", code, p))

# WebSocket 握手探测
import socket, base64, hashlib, os as _os
for port_path in ["/embysocket", "/emby/embysocket"]:
    try:
        s = socket.create_connection(("127.0.0.1", 8096), timeout=3)
        key = base64.b64encode(_os.urandom(16)).decode()
        s.sendall(("GET %s?api_key=%s HTTP/1.1\r\nHost: 127.0.0.1:8096\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n" % (port_path, token, key)).encode())
        resp = s.recv(200).decode(errors="replace").splitlines()[0]
        print("  WS握手 %s → %s" % (port_path, resp))
        s.close()
    except Exception as e:
        print("  WS握手 %s → 失败: %s" % (port_path, str(e)[:60]))

print("\n非 200 端点数（不含可降级 404）:", bad)
