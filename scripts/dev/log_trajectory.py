#!/usr/bin/env python3
"""把 fakemby 的服务日志解析成「真实客户端请求轨迹」回归资产（M3-1）。

为什么要做这个
--------------
M3-1 的目标是让回归用例**来自真实客户端**，而不是人工维护的清单。
人工清单会漂移——M3-2 已经吃过一次亏：v1.4 列的 6 个「实测 404」里 5 个
其实早就随 M2 重构落地了，照着过时清单做了一遍无用功。

而 `dist/server.log` 里记录的正是 Emby Theater 真实发出的请求序列
（`msg="HTTP Request" method=... path=... query=... status=...`）。
把它固化下来，回归资产就不再依赖「谁记得客户端会请求什么」。

做什么
------
1. 从 slog 文本日志里抽取 `msg="HTTP Request"` 记录；
2. 把易变 ID（UUID / 32 位 hex）归一化成 `{userId}` / `{itemId}` / `{id}`
   占位符——真实库里的 ID 换机即失效，占位符才能被测试用种子数据回放；
3. 按首次出现顺序去重（保留顺序 = 保留轨迹语义），并记录每条路上
   真实客户端遇到过的状态码；
4. 输出 JSON 轨迹文件，供 `internal/emby/trajectory_test.go` 回放断言。

用法
----
    python scripts/dev/log_trajectory.py                    # 打印摘要
    python scripts/dev/log_trajectory.py --out FILE         # 写轨迹文件

注意：本脚本依赖本机的 `dist/server.log`（真实客户端会话产物），
与 `audit_client_fields.py` 一样**不进 CI**；CI 跑的是它产出的轨迹文件。
"""

import argparse
import json
import os
import re
import sys

# Windows 控制台默认是 GBK，直接 print 非 ASCII 字符（如 ⚠）会 UnicodeEncodeError。
# 脚本要能在 PowerShell / 重定向到文件两种场景下都正常输出，这里强制 UTF-8。
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, 'reconfigure'):
        _stream.reconfigure(encoding='utf-8')

# logfmt 字段：key=value，值可带引号且引号内可有空格。
FIELD_RE = re.compile(r'(\w+)=("(?:[^"\\]|\\.)*"|[^\s]*)')

UUID_RE = re.compile(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', re.I)
HEX32_RE = re.compile(r'^[0-9a-f]{32}$', re.I)

# 占位符上下文：紧跟在这些路径段后面的 ID 语义明确，便于测试用种子数据替换。
PARENT_TO_PLACEHOLDER = {
    'users': '{userId}',
    'items': '{itemId}',
    'shows': '{itemId}',
    'videos': '{itemId}',
}

# query 里按参数名决定占位符语义（只看值的话 uid=/UserId= 无法区分）。
PLACEHOLDER_BY_PARAM = {
    'userid': '{userId}',
    'uid': '{userId}',
    'itemid': '{itemId}',
    'mediasourceid': '{sourceId}',
}

# 回放时必须剔除的参数：
#   - X-Emby-Token / api_key：测试统一用 X-Emby-Token 请求头鉴权，
#     把一个真实 token 写进轨迹文件既会过期又是凭据泄漏；
#   - exp / sig：直链签名是一次性时效值，静态轨迹无法复用
#     （签名闭环已由 internal/emby/signature_test.go 覆盖）。
DROP_PARAMS = {'x-emby-token', 'api_key', 'exp', 'sig'}


def parse_fields(line):
    """解析一行 slog 文本输出为字段字典（自动去掉引号）。"""
    fields = {}
    for m in FIELD_RE.finditer(line):
        key = m.group(1)
        val = m.group(2)
        if len(val) >= 2 and val.startswith('"') and val.endswith('"'):
            val = val[1:-1]
        fields[key] = val
    return fields


def is_id(segment):
    """判断路径段是否是一个需要归一化的易变 ID。"""
    return bool(UUID_RE.match(segment) or HEX32_RE.match(segment))


def normalize_path(path):
    """把路径里的真实 ID 换成占位符。

    /emby/Users/<uuid>/Items/<hex> -> /emby/Users/{userId}/Items/{itemId}
    """
    segments = path.split('/')
    out = []
    for i, seg in enumerate(segments):
        if is_id(seg):
            parent = segments[i - 1].lower() if i > 0 else ''
            out.append(PARENT_TO_PLACEHOLDER.get(parent, '{id}'))
        else:
            out.append(seg)
    return '/'.join(out)


def normalize_query(query):
    """归一化 query：按参数名替换 ID 占位符，并剔除不可回放的参数。"""
    if not query:
        return ''
    parts = []
    for pair in query.split('&'):
        if not pair:
            continue
        if '=' in pair:
            key, val = pair.split('=', 1)
            if key.lower() in DROP_PARAMS:
                continue
            if is_id(val):
                val = PLACEHOLDER_BY_PARAM.get(key.lower(), '{id}')
            parts.append('%s=%s' % (key, val))
        elif pair.lower() not in DROP_PARAMS:
            parts.append(pair)
    return '&'.join(parts)


def build_trajectory(log_path, client):
    """解析日志文件，返回 (去重后的有序轨迹, 总请求数)。"""
    order = []
    seen = {}
    total = 0

    with open(log_path, encoding='utf-8', errors='replace') as fh:
        for line in fh:
            if 'msg="HTTP Request"' not in line:
                continue
            fields = parse_fields(line)
            method = (fields.get('method') or '').upper()
            path = fields.get('path') or ''
            if not method or not path:
                continue
            total += 1
            try:
                status = int(fields.get('status') or 0)
            except ValueError:
                status = 0

            route = normalize_path(path)
            query = normalize_query(fields.get('query') or '')
            key = (method, route)

            entry = seen.get(key)
            if entry is None:
                entry = {
                    'method': method,
                    'route': route,
                    'query': query,
                    'count': 0,
                    'statuses': [],
                }
                seen[key] = entry
                order.append(entry)
            entry['count'] += 1
            if status and status not in entry['statuses']:
                entry['statuses'].append(status)

    for entry in order:
        entry['statuses'].sort()
    return order, total


def main():
    parser = argparse.ArgumentParser(
        description='把 fakemby 服务日志解析成客户端请求轨迹回归资产（M3-1）')
    parser.add_argument('--log', default=os.path.join('dist', 'server.log'),
                        help='服务日志路径（默认 dist/server.log）')
    parser.add_argument('--out', help='把轨迹 JSON 写到该路径')
    parser.add_argument('--client', default='Emby Theater 3.0.20',
                        help='轨迹来源客户端标注')
    args = parser.parse_args()

    if not os.path.exists(args.log):
        print('日志文件不存在: %s' % args.log, file=sys.stderr)
        return 2

    order, total = build_trajectory(args.log, args.client)

    print('%d 条请求记录 → %d 个去重端点（来源：%s，客户端：%s）\n'
          % (total, len(order), args.log, args.client))
    for i, entry in enumerate(order, 1):
        url = entry['route']
        if entry['query']:
            url += '?' + entry['query']
        bad = [s for s in entry['statuses'] if s >= 400]
        flag = ''
        if bad:
            flag = '   ⚠ 真实客户端遇到过 %s' % ','.join(str(s) for s in bad)
        print('%3d. %-5s %s   x%d%s'
              % (i, entry['method'], url, entry['count'], flag))

    payload = {
        'source': os.path.basename(args.log),
        'client': args.client,
        'generated_by': 'scripts/dev/log_trajectory.py',
        'total_requests': total,
        'requests': order,
    }

    if args.out:
        out_dir = os.path.dirname(args.out)
        if out_dir:
            os.makedirs(out_dir, exist_ok=True)
        with open(args.out, 'w', encoding='utf-8') as fh:
            json.dump(payload, fh, ensure_ascii=False, indent=2)
            fh.write('\n')
        print('\n已写入 %s' % args.out)

    return 0


if __name__ == '__main__':
    sys.exit(main())
