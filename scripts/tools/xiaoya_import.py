#!/usr/bin/env python3
"""小雅（emby.xiaoya.pro）「每日更新」目录 → FakEmby 导入工具。

小雅把 Emby 需要的东西按目录摆好，本工具只做搬运和翻译，不做任何在线元数据抓取：

    每日更新/电影/{地区}/{片名 (年份)}/movie.nfo + poster.jpg + *.strm
    每日更新/电视剧/{地区}/{剧名 (年份)}/tvshow.nfo + Season N/*.nfo|*.strm
    每日更新/动漫/{日本|国漫|美漫|其它}/{年份}/{番名}/（结构同电视剧）

- .nfo  是 tinyMediaManager 写的 Kodi/Emby 风格 XML，直接就是元数据
- .strm 是纯文本文件，内容只有一行直链（xiaoya.host 的 alist 直链），拿来当播放源
- 图片优先用目录里自带的（与站点同域，国内直连稳），没有再退回 nfo 里的 tmdb 图

只依赖标准库，Python 3.9+ 可跑。

用法：
    # 1) 先看看能扫到什么（不解析 nfo，快）
    python scripts/tools/xiaoya_import.py scan --category movie --limit 30

    # 2) 抓 10 部生成 fakemby 的导入 JSON
    python scripts/tools/xiaoya_import.py build --category movie --limit 10 --out dist/xiaoya/movie.json

    # 3) 推送到本地 fakemby（自动建库、自动分批）
    python scripts/tools/xiaoya_import.py push --file dist/xiaoya/movie.json \\
        --server http://localhost:8096 --api-key <admin key>

    # 一把梭（build + push）
    python scripts/tools/xiaoya_import.py run --category movie --limit 10 --server ... --api-key ...
"""
from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from dataclasses import dataclass, field
from typing import Any

DEFAULT_BASE = "https://emby.xiaoya.pro"

# 分类 → (站点相对路径, 库类型, 默认库名)
CATEGORIES: dict[str, tuple[str, str, str]] = {
    "movie": ("每日更新/电影", "movies", "电影"),
    "tv": ("每日更新/电视剧", "tvshows", "电视剧"),
    "anime": ("每日更新/动漫", "tvshows", "动漫"),
}

# fakemby /api/admin/import 接受的图片键（internal/api/admin/import.go）
IMAGE_KEYS = ("Primary", "Backdrop", "Logo", "Thumb", "Banner", "Art", "Disc",
              "Box", "BoxRear", "Menu", "Screenshot")

# 目录里的图片文件名 → 图片键（按顺序优先）
DIR_IMAGE_MAP = (
    ("poster.jpg", "Primary"),
    ("folder.jpg", "Primary"),
    ("fanart.jpg", "Backdrop"),
    ("backdrop.jpg", "Backdrop"),
    ("clearlogo.png", "Logo"),
    ("thumb.jpg", "Thumb"),
    ("landscape.jpg", "Thumb"),
    ("banner.jpg", "Banner"),
)

UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) fakemby-xiaoya-import/1.0"


# --------------------------------------------------------------------------- #
# HTTP
# --------------------------------------------------------------------------- #
class Fetcher:
    """带磁盘缓存、重试、并发的只读抓取器。"""

    def __init__(self, cache_dir: str | None, timeout: float = 20.0,
                 retries: int = 3, workers: int = 8, delay: float = 0.0,
                 source_rewrite: dict[str, str] | None = None):
        self.cache_dir = cache_dir
        self.timeout = timeout
        self.retries = retries
        self.workers = workers
        self.delay = delay
        # 直链前缀重写：小雅 strm 里写的是 xiaoya.host:5678，自建 alist 时可以换成自己的地址
        self.source_rewrite = source_rewrite or {}
        self.stats = {"hit": 0, "miss": 0, "fail": 0}
        if cache_dir:
            os.makedirs(cache_dir, exist_ok=True)

    def _cache_path(self, url: str) -> str:
        return os.path.join(self.cache_dir, hashlib.sha1(url.encode()).hexdigest() + ".txt")

    def fetch_text(self, url: str) -> str:
        if self.cache_dir:
            path = self.cache_path(url)
            if os.path.exists(path):
                self.stats["hit"] += 1
                with open(path, "r", encoding="utf-8", errors="replace") as fh:
                    return fh.read()
        last = None
        for attempt in range(self.retries):
            if self.delay:
                time.sleep(self.delay)
            try:
                req = urllib.request.Request(url, headers={"User-Agent": UA})
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    body = resp.read()
                text = body.decode("utf-8-sig", errors="replace")
                if self.cache_dir:
                    with open(self._cache_path(url), "w", encoding="utf-8") as fh:
                        fh.write(text)
                self.stats["miss"] += 1
                return text
            except Exception as exc:  # noqa: BLE001 - 网络错误一律重试后记账
                last = exc
                time.sleep(0.5 * (attempt + 1))
        self.stats["fail"] += 1
        raise RuntimeError(f"抓取失败 {url}: {last}")

    def fetch_many(self, urls: list[str]) -> dict[str, str]:
        out: dict[str, str] = {}
        if not urls:
            return out
        with concurrent.futures.ThreadPoolExecutor(max_workers=self.workers) as pool:
            futures = {pool.submit(self.fetch_text, u): u for u in urls}
            for fut in concurrent.futures.as_completed(futures):
                url = futures[fut]
                try:
                    out[url] = fut.result()
                except Exception as exc:  # noqa: BLE001
                    print(f"  ! {exc}", file=sys.stderr)
        return out

    def cache_path(self, url: str) -> str:
        return self._cache_path(url)


def join_url(base: str, path: str) -> str:
    """把「相对路径（可能含中文、空格）」拼成可请求的 URL。"""
    quoted = "/".join(urllib.parse.quote(seg, safe="()[]!*'") for seg in path.strip("/").split("/"))
    return f"{base.rstrip('/')}/{quoted}/" if path else base


LINK_RE = re.compile(r'<a\s+href="([^"]+)">(.*?)</a>', re.S)


def list_dir(fetcher: Fetcher, base: str, path: str) -> tuple[list[str], list[str]]:
    """读取 nginx autoindex 目录，返回 (子目录名列表, 文件名列表)，都按名称排序。

    注意：nginx 对长文件名会截断显示文本，所以名字一律从 href 里解，不能信链接文字。
    """
    html = fetcher.fetch_text(join_url(base, path))
    dirs: list[str] = []
    files: list[str] = []
    for href, _text in LINK_RE.findall(html):
        if href in ("../", "./") or href.startswith("?") or href.startswith("/") \
                or href.startswith("http"):
            continue
        name = urllib.parse.unquote(href.rstrip("/"))
        if not name or name == ".." or name == ".":
            continue
        if href.endswith("/"):
            if not name.startswith("."):
                dirs.append(name)
        else:
            files.append(name)
    return sorted(set(dirs)), sorted(set(files))


# --------------------------------------------------------------------------- #
# 条目发现
# --------------------------------------------------------------------------- #
@dataclass
class Entry:
    path: str                 # 相对站点的目录路径
    kind: str                 # movie | series
    name: str = ""
    files: list[str] = field(default_factory=list)
    subdirs: list[str] = field(default_factory=list)


def discover(fetcher: Fetcher, base: str, root: str, limit: int, max_depth: int = 6,
             skip: tuple[str, ...] = ()) -> list[Entry]:
    """从分类根向下找「含 movie.nfo / tvshow.nfo 的目录」，找到即为一条目。"""
    found: list[Entry] = []
    queue: list[tuple[str, int]] = [(root, 0)]
    seen: set[str] = set()
    while queue and len(found) < limit:
        path, depth = queue.pop(0)
        if path in seen or depth > max_depth:
            continue
        seen.add(path)
        try:
            subdirs, files = list_dir(fetcher, base, path)
        except Exception as exc:  # noqa: BLE001
            print(f"  ! 目录读取失败 {path}: {exc}", file=sys.stderr)
            continue

        lowered = {f.lower() for f in files}
        if "movie.nfo" in lowered:
            found.append(Entry(path=path, kind="movie", files=files, subdirs=subdirs))
            continue
        if "tvshow.nfo" in lowered:
            found.append(Entry(path=path, kind="series", files=files, subdirs=subdirs))
            continue
        for sub in subdirs:
            if sub in skip or sub.startswith("."):
                continue
            queue.append((f"{path.rstrip('/')}/{sub}", depth + 1))
    return found


# --------------------------------------------------------------------------- #
# NFO 解析
# --------------------------------------------------------------------------- #
def nfo_text(node: ET.Element | None, tag: str) -> str:
    if node is None:
        return ""
    el = node.find(tag)
    if el is None or el.text is None:
        return ""
    return el.text.strip()


def nfo_texts(node: ET.Element, tag: str) -> list[str]:
    return [(el.text or "").strip() for el in node.findall(tag) if (el.text or "").strip()]


def uniq(items: list[str]) -> list[str]:
    out: list[str] = []
    for it in items:
        if it and it not in out:
            out.append(it)
    return out


def parse_nfo(xml: str) -> ET.Element | None:
    xml = xml.strip().lstrip("﻿")
    if not xml:
        return None
    try:
        return ET.fromstring(xml)
    except ET.ParseError as exc:
        print(f"  ! NFO 解析失败: {exc}", file=sys.stderr)
        return None


def nfo_rating(node: ET.Element) -> float | None:
    ratings = node.find("ratings")
    if ratings is None:
        return None
    candidates = [r for r in ratings.findall("rating") if (r.findtext("value") or "").strip()]
    if not candidates:
        return None
    default = [r for r in candidates if r.get("default") == "true"]
    pick = (default or candidates)[0]
    try:
        value = float(pick.findtext("value") or "")
    except ValueError:
        return None
    maxv = pick.get("max")
    if maxv and maxv not in ("10", "10.0"):
        try:
            value = value / float(maxv) * 10.0
        except (ValueError, ZeroDivisionError):
            return None
    return round(value, 1)


def nfo_provider_ids(node: ET.Element) -> dict[str, str]:
    ids: dict[str, str] = {}
    for el in node.findall("uniqueid"):
        kind = (el.get("type") or "").strip().lower()
        value = (el.text or "").strip()
        if kind and value and kind not in ids:
            ids[kind] = value
    if not ids:
        for key, tag in (("imdb", "id"), ("tmdb", "tmdbid"), ("tvdb", "tvdbid")):
            value = nfo_text(node, tag)
            if value:
                ids[key] = value
    # <id>tt1234567</id> 在 movie.nfo 里其实是 IMDb，但 tvshow.nfo 里可能是 TMDB 数字
    return {k: v for k, v in ids.items() if k in ("imdb", "tmdb", "tvdb")}


def nfo_people(node: ET.Element) -> list[dict[str, str]]:
    people: list[dict[str, str]] = []
    for name in nfo_texts(node, "director"):
        people.append({"name": name, "type": "Director", "role": ""})
    for name in nfo_texts(node, "credits"):
        people.append({"name": name, "type": "Writer", "role": ""})
    for actor in node.findall("actor"):
        name = (actor.findtext("name") or "").strip()
        if not name:
            continue
        people.append({
            "name": name,
            "type": "Actor",
            "role": (actor.findtext("role") or "").strip(),
            "image_url": (actor.findtext("thumb") or "").strip(),
        })
    # 去重（同名的导演/编剧/演员保留第一条）
    out: list[dict[str, str]] = []
    seen: set[tuple[str, str]] = set()
    for p in people:
        key = (p["name"], p["type"])
        if key in seen:
            continue
        seen.add(key)
        out.append(p)
    return out


def nfo_images(node: ET.Element) -> dict[str, str]:
    images: dict[str, str] = {}
    for thumb in node.findall("thumb"):
        url = (thumb.text or "").strip()
        if not url:
            continue
        aspect = (thumb.get("aspect") or "").strip()
        key = {"poster": "Primary", "banner": "Banner", "landscape": "Thumb"}.get(aspect, "Primary")
        images.setdefault(key, url)
    fanart = node.find("fanart")
    if fanart is not None:
        for thumb in fanart.findall("thumb"):
            url = (thumb.text or "").strip()
            if url:
                images.setdefault("Backdrop", url)
                break
    return {k: v for k, v in images.items() if k in IMAGE_KEYS}


def dir_images(base: str, path: str, files: list[str]) -> dict[str, str]:
    """站点自带的图片（与元数据同域，比 tmdb 稳）。"""
    out: dict[str, str] = {}
    lowered = {f.lower(): f for f in files}
    for fname, key in DIR_IMAGE_MAP:
        real = lowered.get(fname)
        if real and key not in out:
            out[key] = join_url(base, f"{path.rstrip('/')}/{real}").rstrip("/")
    return out


def merge_images(primary: dict[str, str], fallback: dict[str, str]) -> dict[str, str]:
    merged = dict(fallback)
    merged.update(primary)
    return {k: v for k, v in merged.items() if v}


# --------------------------------------------------------------------------- #
# 条目构建
# --------------------------------------------------------------------------- #
def source_name(fname: str, link: str) -> str:
    """源名优先用「分辨率 + 压制来源」，认不出来再退回 strm 文件名。"""
    base = os.path.basename(urllib.parse.unquote(urllib.parse.urlparse(link).path))
    res = re.search(r"(?i)(2160p|1080p|720p|480p|4K)", base)
    src = re.search(r"(?i)(WEB-DL|WEBRip|BluRay|BDRip|HDTV|DVDRip|REMUX)", base)
    parts = [p.upper() for p in (res.group(1) if res else "", src.group(1) if src else "") if p]
    if parts:
        return " ".join(parts)
    return os.path.splitext(fname)[0]


def strm_sources(fetcher: Fetcher, base: str, path: str, files: list[str],
                 wanted: set[str] | None = None) -> list[dict[str, Any]]:
    """抓 .strm（每行一个直链）。wanted 为空表示全部。"""
    targets = [f for f in files if f.lower().endswith(".strm")]
    if wanted is not None:
        stems = {os.path.splitext(w)[0].lower() for w in wanted}
        targets = [f for f in targets if os.path.splitext(f)[0].lower() in stems]
    if not targets:
        return []
    urls = [join_url(base, f"{path.rstrip('/')}/{f}").rstrip("/") for f in targets]
    bodies = fetcher.fetch_many(urls)
    sources: list[dict[str, Any]] = []
    for fname, url in zip(targets, urls):
        body = bodies.get(url, "")
        link = body.strip().splitlines()[0].strip() if body.strip() else ""
        if not link.lower().startswith(("http://", "https://")):
            continue
        for old, new in fetcher.source_rewrite.items():
            if link.startswith(old):
                link = new + link[len(old):]
                break
        container = os.path.splitext(urllib.parse.urlparse(link).path)[1].lstrip(".").lower()
        sources.append({
            "name": source_name(fname, link),
            "url": link,
            **({"container": container} if container else {}),
        })
    return sources


def build_movie(fetcher: Fetcher, base: str, entry: Entry) -> dict[str, Any] | None:
    nfo_url = join_url(base, f"{entry.path.rstrip('/')}/movie.nfo").rstrip("/")
    node = parse_nfo(fetcher.fetch_text(nfo_url))
    if node is None:
        return None
    name = nfo_text(node, "title") or os.path.basename(entry.path.rstrip("/"))
    item: dict[str, Any] = {
        "name": name,
        "type": "Movie",
        "overview": nfo_text(node, "plot") or nfo_text(node, "outline"),
        "genres": uniq(nfo_texts(node, "genre")),
        "tags": uniq(nfo_texts(node, "tag")),
        "studios": uniq(nfo_texts(node, "studio")),
        "countries": uniq(nfo_texts(node, "country")),
        "people": nfo_people(node),
        "images": merge_images(dir_images(base, entry.path, entry.files), nfo_images(node)),
        "sources": strm_sources(fetcher, base, entry.path, entry.files, None),
    }
    original = nfo_text(node, "originaltitle")
    if original and original != name:
        item["original_title"] = original
    for key, tag in (("year", "year"), ("runtime_minutes", "runtime")):
        raw = nfo_text(node, tag)
        if raw.isdigit():
            item[key] = int(raw)
    premiered = nfo_text(node, "premiered") or nfo_text(node, "releasedate")
    if premiered:
        item["premiere_date"] = premiered
    rating = nfo_rating(node)
    if rating is not None:
        item["community_rating"] = rating
    official = nfo_text(node, "certification") or nfo_text(node, "mpaa")
    if official:
        item["official_rating"] = official
    tagline = nfo_text(node, "tagline")
    if tagline:
        item["taglines"] = [tagline]
    providers = nfo_provider_ids(node)
    if providers:
        item["ProviderIds"] = providers
    return item


SEASON_RE = re.compile(r"^season\s*(\d+)$", re.I)


def build_series(fetcher: Fetcher, base: str, entry: Entry, max_seasons: int,
                 max_episodes: int) -> dict[str, Any] | None:
    nfo_url = join_url(base, f"{entry.path.rstrip('/')}/tvshow.nfo").rstrip("/")
    node = parse_nfo(fetcher.fetch_text(nfo_url))
    if node is None:
        return None
    name = nfo_text(node, "title") or os.path.basename(entry.path.rstrip("/"))
    item: dict[str, Any] = {
        "name": name,
        "type": "Series",
        "overview": nfo_text(node, "plot") or nfo_text(node, "outline"),
        "genres": uniq(nfo_texts(node, "genre")),
        "tags": uniq(nfo_texts(node, "tag")),
        "studios": uniq(nfo_texts(node, "studio")),
        "people": nfo_people(node),
        "images": merge_images(dir_images(base, entry.path, entry.files), nfo_images(node)),
    }
    original = nfo_text(node, "originaltitle")
    if original and original != name:
        item["original_title"] = original
    if nfo_text(node, "year").isdigit():
        item["year"] = int(nfo_text(node, "year"))
    premiered = nfo_text(node, "premiered")
    if premiered:
        item["premiere_date"] = premiered
    rating = nfo_rating(node)
    if rating is not None:
        item["community_rating"] = rating
    official = nfo_text(node, "certification") or nfo_text(node, "mpaa")
    if official:
        item["official_rating"] = official
    providers = nfo_provider_ids(node)
    if providers:
        item["ProviderIds"] = providers

    season_dirs = sorted(
        (m.group(1), sub) for sub in entry.subdirs if (m := SEASON_RE.match(sub))
    )
    if max_seasons > 0:
        season_dirs = season_dirs[:max_seasons]

    root_images = {f.lower(): f for f in entry.files}
    seasons: list[dict[str, Any]] = []
    for number, sub in season_dirs:
        season_no = int(number)
        season_path = f"{entry.path.rstrip('/')}/{sub}"
        try:
            _subdirs, files = list_dir(fetcher, base, season_path)
        except Exception as exc:  # noqa: BLE001
            print(f"  ! 季目录读取失败 {season_path}: {exc}", file=sys.stderr)
            continue
        season_img: dict[str, str] = {}
        for candidate in (f"season{season_no:02d}-poster.jpg", f"season{season_no}-poster.jpg"):
            real = root_images.get(candidate)
            if real:
                season_img["Primary"] = join_url(base, f"{entry.path.rstrip('/')}/{real}").rstrip("/")
                break
        episodes = build_episodes(fetcher, base, season_path, files, max_episodes)
        if not episodes:
            continue
        seasons.append({
            "name": f"第 {season_no} 季",
            "season_number": season_no,
            **({"images": season_img} if season_img else {}),
            "episodes": episodes,
        })
    if not seasons:
        return None
    item["seasons"] = seasons
    return item


def build_episodes(fetcher: Fetcher, base: str, season_path: str, files: list[str],
                   max_episodes: int) -> list[dict[str, Any]]:
    # season.nfo 是「季」自己的元数据，不是一集，别混进来
    nfos = sorted(f for f in files
                  if f.lower().endswith(".nfo") and f.lower() != "season.nfo")
    if max_episodes > 0:
        nfos = nfos[:max_episodes]
    if not nfos:
        return []
    urls = [join_url(base, f"{season_path.rstrip('/')}/{f}").rstrip("/") for f in nfos]
    bodies = fetcher.fetch_many(urls)
    # 一个 nfo 只认同名 .strm：逐个取，别跨集串台（源名已不是文件名，不能按 name 归位）
    sources_by_stem = {
        os.path.splitext(f)[0]: strm_sources(fetcher, base, season_path, files,
                                             wanted={os.path.splitext(f)[0]})
        for f in nfos
    }

    episodes: list[dict[str, Any]] = []
    for fname, url in zip(nfos, urls):
        node = parse_nfo(bodies.get(url, ""))
        if node is None:
            continue
        stem = os.path.splitext(fname)[0]
        season_raw = nfo_text(node, "season")
        episode_raw = nfo_text(node, "episode")
        ep: dict[str, Any] = {
            "name": nfo_text(node, "title") or stem,
            "overview": nfo_text(node, "plot") or nfo_text(node, "outline"),
        }
        if episode_raw.isdigit():
            ep["episode_number"] = int(episode_raw)
        if season_raw.isdigit():
            ep["season_number"] = int(season_raw)
        if nfo_text(node, "runtime").isdigit():
            ep["runtime_minutes"] = int(nfo_text(node, "runtime"))
        aired = nfo_text(node, "aired") or nfo_text(node, "premiered")
        if aired:
            ep["premiere_date"] = aired
        images = nfo_images(node)
        if images:
            ep["images"] = images
        providers = nfo_provider_ids(node)
        if providers:
            ep["ProviderIds"] = providers
        srcs = sources_by_stem.get(stem) or []
        if srcs:
            ep["sources"] = srcs
        episodes.append(ep)
    episodes.sort(key=lambda e: (e.get("episode_number", 0)))
    return episodes


def collect_source_urls(items: list[dict[str, Any]]) -> list[str]:
    urls: list[str] = []
    for item in items:
        urls.extend(s["url"] for s in item.get("sources", []))
        for season in item.get("seasons", []):
            for ep in season.get("episodes", []):
                urls.extend(s["url"] for s in ep.get("sources", []))
    return urls


def probe_sources(items: list[dict[str, Any]], limit: int) -> None:
    """导入前体检：HEAD 抽查直链，看源站到底能不能连。"""
    urls = collect_source_urls(items)[:limit]
    if not urls:
        return
    print(f"  直链体检（{len(urls)} 条）：")
    for url in urls:
        safe = urllib.parse.quote(url, safe=":/?&=%#")
        try:
            req = urllib.request.Request(safe, method="HEAD", headers={"User-Agent": UA})
            with urllib.request.urlopen(req, timeout=20) as resp:
                status, note = resp.status, resp.headers.get("Content-Length") or ""
        except urllib.error.HTTPError as exc:
            status, note = exc.code, ""
        except Exception as exc:  # noqa: BLE001
            status, note = "ERR", str(exc)[:60]
        print(f"    {status} {note} {url[:70]}")


def build_items(fetcher: Fetcher, base: str, entries: list[Entry], max_seasons: int,
                max_episodes: int) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    for idx, entry in enumerate(entries, 1):
        try:
            item = (build_movie(fetcher, base, entry) if entry.kind == "movie"
                    else build_series(fetcher, base, entry, max_seasons, max_episodes))
        except Exception as exc:  # noqa: BLE001
            print(f"  ! 条目构建失败 {entry.path}: {exc}", file=sys.stderr)
            continue
        if item:
            items.append(item)
            print(f"  [{idx}/{len(entries)}] {item['type']:<6} {item['name']}")
    return items


# --------------------------------------------------------------------------- #
# 推送
# --------------------------------------------------------------------------- #
def api_json(server: str, api_key: str, method: str, path: str,
             payload: dict | None = None) -> tuple[int, Any]:
    url = f"{server.rstrip('/')}{path}"
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Content-Type": "application/json",
        "X-Api-Key": api_key,
    })
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")
        return exc.code, _safe_json(raw)
    return 200, _safe_json(raw)


def _safe_json(raw: str) -> Any:
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return raw


def ensure_library(server: str, api_key: str, name: str, lib_type: str,
                   sort_order: int) -> None:
    code, body = api_json(server, api_key, "GET", "/api/admin/libraries")
    if code == 200 and isinstance(body, list):
        for lib in body:
            # 库对象直出 database.Library，字段名是大写 Name/Type
            if (lib.get("name") or lib.get("Name")) == name:
                print(f"  库已存在：{name} ({lib.get('type') or lib.get('Type')})")
                return
    code, body = api_json(server, api_key, "POST", "/api/admin/libraries",
                          {"name": name, "type": lib_type, "sort_order": sort_order})
    if code in (200, 201):
        print(f"  建库：{name} ({lib_type})")
    else:
        print(f"  ! 建库失败 {code}: {body}", file=sys.stderr)


def count_nodes(item: dict[str, Any]) -> int:
    total = 1
    for season in item.get("seasons", []):
        total += 1 + len(season.get("episodes", []))
    return total


def chunk_items(items: list[dict[str, Any]], max_items: int = 200,
                max_nodes: int = 3000, max_bytes: int = 6 * 1024 * 1024) -> list[list[dict]]:
    """按 fakemby 的上限切批：1000 items / 5000 nodes / 8MB 请求体。"""
    batches: list[list[dict]] = []
    cur: list[dict] = []
    cur_nodes = 0
    for item in items:
        nodes = count_nodes(item)
        size = len(json.dumps(item, ensure_ascii=False).encode("utf-8"))
        if cur and (len(cur) + 1 > max_items or cur_nodes + nodes > max_nodes
                    or size > max_bytes):
            batches.append(cur)
            cur, cur_nodes = [], 0
        cur.append(item)
        cur_nodes += nodes
        if cur_nodes >= max_nodes or len(cur) >= max_items:
            batches.append(cur)
            cur, cur_nodes = [], 0
    if cur:
        batches.append(cur)
    return batches


def push_payload(server: str, api_key: str, payload: dict, dry_run: bool) -> None:
    lib_name = payload.get("library", "")
    items = payload.get("items", [])
    lib_type = payload.get("library_type")
    if not lib_type:
        # 老文件里没记库类型：有剧集就是剧集库
        lib_type = "tvshows" if any(i.get("type") == "Series" for i in items) else "movies"
    if not dry_run:
        ensure_library(server, api_key, lib_name, lib_type, 0)
    for idx, batch in enumerate(chunk_items(items), 1):
        body = {"library": lib_name, "items": batch}
        size = len(json.dumps(body, ensure_ascii=False).encode("utf-8"))
        print(f"  批次 {idx}: {len(batch)} 条 / {size // 1024} KB")
        if dry_run:
            continue
        code, resp = api_json(server, api_key, "POST", "/api/admin/import", body)
        print(f"    -> HTTP {code}: {json.dumps(resp, ensure_ascii=False)[:400]}")


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
def cmd_scan(args: argparse.Namespace) -> int:
    fetcher = Fetcher(args.cache_dir, args.timeout, args.retries, args.workers)
    for category in args.category:
        rel, _lib_type, default_name = CATEGORIES[category]
        print(f"[{category}] {rel} (limit={args.limit})")
        entries = discover(fetcher, args.base_url, rel, args.limit,
                           args.max_depth, tuple(args.skip))
        for entry in entries:
            print(f"  {entry.kind:<6} {entry.path}")
        print(f"  小计 {len(entries)} 条；缓存命中 {fetcher.stats['hit']} / 抓取 {fetcher.stats['miss']}")
    return 0


def build_payload(args: argparse.Namespace, category: str) -> dict[str, Any]:
    rel, lib_type, default_name = CATEGORIES[category]
    fetcher = Fetcher(args.cache_dir, args.timeout, args.retries, args.workers,
                      source_rewrite=args.source_rewrite)
    print(f"[{category}] {rel} limit={args.limit}")
    # 多抓一些以应对解析失败，抓够 limit 个有效条目为止
    want = args.limit
    entries = discover(fetcher, args.base_url, rel, want + args.offset,
                       args.max_depth, tuple(args.skip))
    if args.offset:
        entries = entries[args.offset:]
    items: list[dict[str, Any]] = []
    while len(items) < want and entries:
        batch = entries[:max(want - len(items), 1)]
        entries = entries[len(batch):]
        items.extend(build_items(fetcher, args.base_url, batch, args.max_seasons, args.max_episodes))
    items = items[:want]
    return {
        "library": args.library or default_name,
        "library_type": lib_type,
        "items": items,
    }


def cmd_build(args: argparse.Namespace) -> int:
    for category in args.category:
        payload = build_payload(args, category)
        path = args.out
        if len(args.category) > 1:
            path = args.out.replace(".json", f"_{category}.json") if args.out.endswith(".json") \
                else f"{args.out}_{category}.json"
        if path:
            os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                json.dump(payload, fh, ensure_ascii=False, indent=2)
            print(f"  写出 {path}（{len(payload['items'])} 条）")
        else:
            json.dump(payload, sys.stdout, ensure_ascii=False, indent=2)
        if args.check_sources:
            probe_sources(payload["items"], args.check_sources)
    return 0


def cmd_push(args: argparse.Namespace) -> int:
    with open(args.file, "r", encoding="utf-8") as fh:
        payload = json.load(fh)
    if args.library:
        payload["library"] = args.library
    if not payload.get("library"):
        print("缺少库名：用 --library 指定，或在 JSON 里带 library 字段", file=sys.stderr)
        return 2
    print(f"推送 {args.file} → {args.server} 库[{payload['library']}]")
    push_payload(args.server, args.api_key or os.environ.get("FAKEMBY_ADMIN_API_KEY", ""),
                 payload, args.dry_run)
    return 0


def cmd_run(args: argparse.Namespace) -> int:
    key = args.api_key or os.environ.get("FAKEMBY_ADMIN_API_KEY", "")
    if not key and not args.dry_run:
        print("缺少管理密钥：--api-key 或 FAKEMBY_ADMIN_API_KEY", file=sys.stderr)
        return 2
    for category in args.category:
        payload = build_payload(args, category)
        if args.out:
            path = args.out.replace(".json", f"_{category}.json") if args.out.endswith(".json") \
                else f"{args.out}_{category}.json"
            os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                json.dump({k: v for k, v in payload.items() if k != "library_type"},
                          fh, ensure_ascii=False, indent=2)
            print(f"  写出 {path}")
        if args.check_sources:
            probe_sources(payload["items"], args.check_sources)
        print(f"推送 [{payload['library']}] {len(payload['items'])} 条")
        push_payload(args.server, key, payload, args.dry_run)
    return 0


def main(argv: list[str] | None = None) -> int:
    # Windows 控制台可能是 GBK，中文片名一律按 UTF-8 输出，乱码也比抛异常强
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8", errors="replace")  # type: ignore[attr-defined]
        except Exception:  # noqa: BLE001
            pass
    parser = argparse.ArgumentParser(description="小雅 每日更新 → FakEmby 导入工具")
    parser.add_argument("command", choices=("scan", "build", "push", "run"))
    parser.add_argument("--base-url", default=DEFAULT_BASE)
    parser.add_argument("--category", default="movie",
                        help="逗号分隔：movie,tv,anime 或 all")
    parser.add_argument("--limit", type=int, default=10, help="每个分类抓几条")
    parser.add_argument("--offset", type=int, default=0, help="跳过前 N 个条目（分批用）")
    parser.add_argument("--max-depth", type=int, default=6)
    parser.add_argument("--max-seasons", type=int, default=0, help="每部剧最多几季，0=不限")
    parser.add_argument("--max-episodes", type=int, default=0, help="每季最多几集，0=不限")
    parser.add_argument("--skip", default="", help="跳过的目录名，逗号分隔")
    parser.add_argument("--library", default="", help="覆盖默认库名")
    parser.add_argument("--out", default="", help="输出 JSON 路径")
    parser.add_argument("--file", default="", help="push 时读取的 JSON")
    parser.add_argument("--server", default="http://localhost:8096")
    parser.add_argument("--api-key", default="")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--source-url-rewrite", action="append", default=[],
                        metavar="OLD=NEW",
                        help="直链前缀重写，可多次；例：http://xiaoya.host:5678=http://192.168.1.5:5678")
    parser.add_argument("--check-sources", type=int, default=0,
                        help="导入前 HEAD 抽查 N 条直链（0=不检查）")
    parser.add_argument("--cache-dir", default=os.path.join("dist", "cache", "xiaoya"))
    parser.add_argument("--no-cache", action="store_true")
    parser.add_argument("--timeout", type=float, default=20.0)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--workers", type=int, default=8)
    args = parser.parse_args(argv)

    if args.category == "all":
        args.category = ["movie", "tv", "anime"]
    else:
        args.category = [c.strip() for c in args.category.split(",") if c.strip()]
    for cat in args.category:
        if cat not in CATEGORIES:
            parser.error(f"未知分类 {cat}，可选：{','.join(CATEGORIES)}")
    args.skip = [s for s in args.skip.split(",") if s]
    rewrite: dict[str, str] = {}
    for rule in args.source_url_rewrite:
        if "=" not in rule:
            parser.error(f"--source-url-rewrite 需要 OLD=NEW 格式，收到：{rule}")
        old, new = rule.split("=", 1)
        rewrite[old] = new
    args.source_rewrite = rewrite
    if args.no_cache:
        args.cache_dir = None

    return {"scan": cmd_scan, "build": cmd_build, "push": cmd_push, "run": cmd_run}[args.command](args)


if __name__ == "__main__":
    sys.exit(main())
