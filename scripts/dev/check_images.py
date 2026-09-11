import sqlite3, urllib.request, ssl, json

db = sqlite3.connect('dist/fakemby.db')
rows = db.execute("SELECT id, item_id, type, idx, url FROM images").fetchall()
print("images 表共 %d 条" % len(rows))

# 本机禁代理 + 忽略证书（TUN 会劫持 TLS，证书报错不代表 URL 死，404 才是死）
ctx = ssl._create_unverified_context()

dead = []
alive = []
for iid, item_id, typ, idx, url in rows:
    status = "?"
    try:
        rq = urllib.request.Request(url, method="HEAD",
                                    headers={"User-Agent": "Emby Theater"})
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                             urllib.request.HTTPSHandler(context=ctx))
        with opener.open(rq, timeout=10) as resp:
            status = resp.status
            alive.append((iid, item_id, typ, idx, url))
    except urllib.error.HTTPError as e:
        status = e.code
        if e.code >= 400:
            dead.append((iid, item_id, typ, idx, url, e.code))
        else:
            alive.append((iid, item_id, typ, idx, url))
    except Exception as e:
        # HEAD 不被支持时用 GET 重试
        try:
            rq = urllib.request.Request(url, headers={"User-Agent": "Emby Theater"})
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                 urllib.request.HTTPSHandler(context=ctx))
            with opener.open(rq, timeout=10) as resp:
                resp.read(1024)
                status = resp.status
                alive.append((iid, item_id, typ, idx, url))
        except Exception as e2:
            status = str(e2)[:60]
            dead.append((iid, item_id, typ, idx, url, str(e2)[:60]))

print("\n=== 可达 (%d) ===" % len(alive))
for iid, item_id, typ, idx, url in alive:
    print("  [%s/%s] %s" % (typ, idx, url[:90]))

print("\n=== 死链 (%d) ===" % len(dead))
for iid, item_id, typ, idx, url, err in dead:
    print("  [%s/%s] %s\n      原因: %s" % (typ, idx, url[:90], err))

with open('dist/dead_images.json', 'w', encoding='utf-8') as f:
    json.dump([{"id": d[0], "item_id": d[1], "type": d[2], "idx": d[3], "url": d[4], "err": d[5]} for d in dead],
              f, ensure_ascii=False, indent=1)
print("\n死链清单已写 dist/dead_images.json")
