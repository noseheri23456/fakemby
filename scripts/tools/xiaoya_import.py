#!/usr/bin/env python3
"""小雅（emby.xiaoya.pro）→ FakEmby 导入工具（支持断点续传 + 日常增量）。

小雅把 Emby 需要的东西按目录摆好，本工具只做搬运和翻译，不做任何在线元数据抓取：

    {分类根}/{...}/{片名 (年份)}/movie.nfo|tvshow.nfo + poster.jpg + *.strm + *.srt
    {分类根}/{...}/{剧名 (年份)}/tvshow.nfo + Season N/*.nfo|*.strm|*.ass

- .nfo  是 tinyMediaManager 写的 Kodi/Emby 风格 XML，直接就是元数据
- .strm 是纯文本文件，内容只有一行直链（xiaoya.host 的 alist 直链），拿来当播放源
- 图片优先用目录里自带的（与站点同域，国内直连稳），没有再退回 nfo 里的 tmdb 图

相比初版补齐了三件事：

1. **完整元数据**：字幕（.srt/.ass/.ssa/.vtt）、音轨语言、外部链接（IMDb/TMDB/TVDB）、
   剧集缩略图、季海报、以片名命名的图片（`{片名}-poster.jpg`）与散装目录
   （一个目录里平铺多部片子，如 `电影/4K系列/*`、`纪录片/NHK`）。
2. **断点续传**：进度写在 state 文件里，条目构建结果落 JSONL。中断后重跑只做没做完的，
   Ctrl+C 也会先存盘再退出。
3. **日常增量**：每条记录目录清单签名（文件名 + mtime + 大小），签名没变就直接复用上次的
   结果，签名变了才重新抓取；推送侧记 pushed_sig，只推没推过或更新过的条目。

只依赖标准库，Python 3.9+ 可跑。

常见用法：

    # 看看能扫到什么（不解析 nfo，快）
    python scripts/tools/xiaoya_import.py scan --category movie --limit 30

    # 抓 10 部，结果落到 dist/xiaoya（默认带断点续传）
    python scripts/tools/xiaoya_import.py build --category movie --limit 10

    # 一把梭：扫描 + 构建 + 推送（第二次跑只会处理变化的）
    python scripts/tools/xiaoya_import.py run --category movie --limit 10 \\
        --server http://localhost:8096 --api-key <admin key>

    # 日常增量：只处理最近 7 天动过的，顺带把超过一天的目录列表缓存刷新掉
    python scripts/tools/xiaoya_import.py run --category all --since-days 7 --cache-ttl 86400

    # 看进度 / 重来
    python scripts/tools/xiaoya_import.py status
    python scripts/tools/xiaoya_import.py reset --category movie

推送目标库由 --library 指定；不给就用分类的默认库名。
"""
from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
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
# 站点根目录下还有「音乐」和「📺画质演示测试」，前者 fakemby 的 ImportItem.Type
# 只认 Movie/Series/Season/Episode（导入不了音频），后者是测试片，都不在表里。
CATEGORIES: dict[str, tuple[str, str, str]] = {
    "movie": ("每日更新/电影", "movies", "电影"),
    "tv": ("每日更新/电视剧", "tvshows", "电视剧"),
    "anime": ("每日更新/动漫", "tvshows", "动漫"),
    "anime_movie": ("每日更新/动漫剧场版", "movies", "动漫剧场版"),
    "lib_movie": ("电影", "movies", "电影库"),
    "lib_tv": ("电视剧", "tvshows", "电视剧库"),
    "lib_anime": ("动漫", "tvshows", "动漫库"),
    "doc": ("纪录片", "movies", "纪录片"),
    "doc_scraped": ("纪录片（已刮削）", "movies", "纪录片（已刮削）"),
    "variety": ("综艺", "tvshows", "综艺"),
}

# 「每日更新」四个分类，日常增量最常跑的就是它们
DAILY_CATEGORIES = ("movie", "tv", "anime", "anime_movie")

# fakemby /api/admin/import 接受的图片键（internal/api/admin/import.go）
IMAGE_KEYS = ("Primary", "Backdrop", "Logo", "Thumb", "Banner", "Art", "Disc",
              "Box", "BoxRear", "Menu", "Screenshot")

IMAGE_EXTS = ("jpg", "jpeg", "png", "webp")

# 目录级图片文件名 → 图片键（整目录共用的那种，按顺序优先）
DIR_IMAGE_MAP = (
    ("poster.jpg", "Primary"),
    ("folder.jpg", "Primary"),
    ("cover.jpg", "Primary"),
    ("fanart.jpg", "Backdrop"),
    ("backdrop.jpg", "Backdrop"),
    ("clearlogo.png", "Logo"),
    ("logo.png", "Logo"),
    ("landscape.jpg", "Thumb"),
    ("thumb.jpg", "Thumb"),
    ("banner.jpg", "Banner"),
)

# 以片名打头的图片：`{stem}-poster.jpg`、`{stem}-fanart.jpg`、剧集 `{stem}-thumb.jpg`
STEM_IMAGE_MAP = (
    ("-poster", "Primary"),
    ("-cover", "Primary"),
    ("-fanart", "Backdrop"),
    ("-backdrop", "Backdrop"),
    ("-thumb", "Thumb"),
    ("-landscape", "Thumb"),
    ("-banner", "Banner"),
    ("-clearlogo", "Logo"),
    ("-logo", "Logo"),
    ("-clearart", "Art"),
    ("-disc", "Disc"),
)

SUBTITLE_EXTS = (".srt", ".ass", ".ssa", ".vtt", ".sub")

# 字幕文件名里的语言标记。小雅以中字为主，认不出来时用 --sub-lang-default。
SUB_LANG_RULES = (
    (r"(?:[._\[\-\s])(?:zh[-_]?cn|zh[-_]?hans|chs|chi|zho|sc|简体|简中|中文)(?:[._\[\-\s]|$)", "zho"),
    (r"(?:[._\[\-\s])(?:zh[-_]?tw|zh[-_]?hk|zh[-_]?hant|cht|tc|繁體|繁体|粤[语語])(?:[._\[\-\s]|$)", "zho"),
    (r"(?:[._\[\-\s])(?:en[g]?|english|英[语語文])(?:[._\[\-\s]|$)", "eng"),
    (r"(?:[._\[\-\s])(?:jp|jpn|japanese|日[语語文])(?:[._\[\-\s]|$)", "jpn"),
    (r"(?:[._\[\-\s])(?:ko|kor|korean|韩[语語文]|韓[語文])(?:[._\[\-\s]|$)", "kor"),
    (r"(?:[._\[\-\s])(?:ru|rus|russian|俄[语語文])(?:[._\[\-\s]|$)", "rus"),
    (r"(?:[._\[\-\s])(?:fr|fre|fra|french|法[语語文])(?:[._\[\-\s]|$)", "fra"),
    (r"(?:[._\[\-\s])(?:es|spa|spanish|西班牙[语語])(?:[._\[\-\s]|$)", "spa"),
)

UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) fakemby-xiaoya-import/2.0"

MONTHS = {m: i for i, m in enumerate(
    ("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"), 1)}

STATE_VERSION = 2


def now_iso() -> str:
    return dt.datetime.now().replace(microsecond=0).isoformat()


def log(msg: str) -> None:
    print(msg, flush=True)


# --------------------------------------------------------------------------- #
# HTTP
# --------------------------------------------------------------------------- #
class Fetcher:
    """带磁盘缓存、重试、并发的只读抓取器。

    目录列表有「保鲜期」（--cache-ttl），nfo/strm 之类的内容文件默认永久缓存
    ——小雅不会去改一个已发布条目的 nfo 内容，变了也是整目录签名先变。
    """

    def __init__(self, cache_dir: str | None, timeout: float = 20.0,
                 retries: int = 3, workers: int = 8, delay: float = 0.0,
                 source_rewrite: dict[str, str] | None = None,
                 dir_ttl: float | None = None):
        self.cache_dir = cache_dir
        self.timeout = timeout
        self.retries = retries
        self.workers = workers
        self.delay = delay
        self.source_rewrite = source_rewrite or {}
        self.dir_ttl = dir_ttl
        self.stats = {"hit": 0, "miss": 0, "fail": 0}
        if cache_dir:
            os.makedirs(cache_dir, exist_ok=True)

    def _cache_path(self, url: str) -> str:
        return os.path.join(self.cache_dir, hashlib.sha1(url.encode()).hexdigest() + ".txt")

    def _fresh(self, path: str, ttl: float | None) -> bool:
        if ttl is None:
            return True
        try:
            return (time.time() - os.path.getmtime(path)) < ttl
        except OSError:
            return False

    def fetch_text(self, url: str, ttl: float | None = None) -> str:
        if self.cache_dir:
            path = self._cache_path(url)
            if os.path.exists(path) and self._fresh(path, ttl):
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

    def fetch_dir(self, url: str) -> str:
        """目录列表：受 --cache-ttl 约束，日常增量靠它发现新条目。"""
        return self.fetch_text(url, ttl=self.dir_ttl)

    def fetch_many(self, urls: list[str], ttl: float | None = None) -> dict[str, str]:
        out: dict[str, str] = {}
        if not urls:
            return out
        with concurrent.futures.ThreadPoolExecutor(max_workers=self.workers) as pool:
            futures = {pool.submit(self.fetch_text, u, ttl): u for u in urls}
            for fut in concurrent.futures.as_completed(futures):
                url = futures[fut]
                try:
                    out[url] = fut.result()
                except Exception as exc:  # noqa: BLE001
                    self.stats["fail"] += 1
                    print(f"  ! {exc}", file=sys.stderr, flush=True)
        return out

    def fetch_dirs(self, urls: list[str]) -> dict[str, str]:
        return self.fetch_many(urls, ttl=self.dir_ttl)


def join_url(base: str, path: str) -> str:
    """把「相对路径（可能含中文、空格）」拼成可请求的 URL。

    注意 safe 里**不能**放宽空格：urllib 会拒绝含空格的 URL（control characters）。
    """
    segs = [s for s in path.strip("/").split("/") if s]
    quoted = "/".join(urllib.parse.quote(seg, safe="()[]!*'") for seg in segs)
    return f"{base.rstrip('/')}/{quoted}/" if quoted else base.rstrip("/") + "/"


def file_url(base: str, path: str, name: str) -> str:
    return join_url(base, f"{path.rstrip('/')}/{name}").rstrip("/")


# --------------------------------------------------------------------------- #
# 目录列表
# --------------------------------------------------------------------------- #
@dataclass
class DirItem:
    name: str
    is_dir: bool
    mtime: str = ""     # 原样保留 nginx 的字符串，如 "17-Sep-2026 17:14"
    size: str = ""      # 目录是 "-"，文件是字节数


LINK_RE = re.compile(
    r'<a\s+href="([^"]+)"[^>]*>(.*?)</a>\s*'
    r'(?:(\d{2}-[A-Za-z]{3}-\d{4}\s+\d{2}:\d{2}))?\s*(\S+)?', re.S)


def parse_listing(html: str) -> list[DirItem]:
    """解析 nginx autoindex，返回本层条目。

    注意：nginx 对长文件名会截断显示文本，所以名字一律从 href 里解，不能信链接文字。
    """
    out: list[DirItem] = []
    seen: set[str] = set()
    for href, _text, mtime, size in LINK_RE.findall(html):
        if href.startswith(("?", "/", "http", "#")):
            continue
        name = urllib.parse.unquote(href.rstrip("/"))
        if not name or name in ("..", ".") or name in seen:
            continue
        seen.add(name)
        out.append(DirItem(name=name, is_dir=href.endswith("/"),
                           mtime=mtime or "", size=size or ""))
    return out


def parse_mtime(raw: str) -> dt.datetime | None:
    """nginx 的目录时间是英文月份，strptime 受 locale 影响，这里手写映射。"""
    try:
        day, mon, rest = raw.split("-", 2)
        year, clock = rest.split()
        hour, minute = clock.split(":")
        return dt.datetime(int(year), MONTHS[mon[:3].title()], int(day), int(hour), int(minute))
    except Exception:  # noqa: BLE001
        return None


# --------------------------------------------------------------------------- #
# 条目发现
# --------------------------------------------------------------------------- #
@dataclass
class Entry:
    path: str                  # 相对站点的目录路径
    kind: str                  # movie | series | loose
    items: list[DirItem] = field(default_factory=list)
    stems: list[str] = field(default_factory=list)   # loose 模式下每个 stem 一条
    sig: str = ""
    mtime: str = ""

    @property
    def files(self) -> list[str]:
        return [i.name for i in self.items if not i.is_dir]

    @property
    def subdirs(self) -> list[str]:
        return [i.name for i in self.items if i.is_dir]


SEASON_RE = re.compile(r"^season\s*(\d+)$", re.I)
SPECIALS_RE = re.compile(r"^(specials?|特别篇|ova|oad)$", re.I)

# 「像一集」的 stem：整串只有集号（可带音轨/清晰度尾巴）。
# 用来把「集平铺在剧目录里」和「散装的一堆电影」分开——后者虽然也可能带 tvshow.nfo，
# 但 stem 是完整片名（`【历史影像】120年前的清朝` vs `01粤语`）。
EPISODE_STEM_RE = re.compile(r"""(?ix)^\s*(?:
      s\d{1,2}\s*[._\-]?\s*e\d{1,3}                                  # S01E02
    | ep(?:isode)?\s*[._\-]?\s*\d{1,3}                               # Episode 2 / EP02
    | 第\s*(?:\d{1,4}|[一二三四五六七八九十两]+)\s*[集话話回]                  # 第 2 集 / 第一集
    | \d{1,3}\s*[._\- ]?(?:粤语|国语|普通话|日语|韩语|英语|台配|中字|原声|
        外挂|内嵌|1080p|720p|2160p|4k|bd|hd|web-?dl|hdr|sdr)?        # 01粤语 / 02
  )\s*(?:\[[^\]]*\]|\([^)]*\))?\s*$""")


def entry_signature(items: list[DirItem]) -> str:
    """目录清单签名：文件名 + 是否目录 + mtime + 大小。

    子目录的 mtime 也在里面，所以季里加了新集（季目录 mtime 变）会带动整部剧的签名变化，
    不需要为了判变而把每个季目录都拉一遍。
    """
    payload = "\n".join(sorted(f"{i.name}|{'d' if i.is_dir else 'f'}|{i.mtime}|{i.size}"
                               for i in items))
    return hashlib.sha1(payload.encode("utf-8")).hexdigest()[:16]


def classify(path: str, items: list[DirItem], allow_no_source: bool) -> Entry | None:
    """判断一个目录是不是「一条（或多条）可导入的条目」。

    站点上实际存在四种摆法：
      1. 整目录一部电影：movie.nfo（或 tvshow.nfo 但没有季）+ 若干 .strm
      2. 整目录一部剧：tvshow.nfo + Season N/
      3. 目录名就是片名：`一家之主 (2022)/一家之主 (2022).nfo`
      4. 散装：一个目录里平铺多部片子，靠 stem 配对（`电影/4K系列/DC系列`、`纪录片/NHK`）
    """
    files = [i for i in items if not i.is_dir]
    dirs = [i for i in items if i.is_dir]
    lowered = {f.name.lower(): f for f in files}
    nfos = [f for f in files if f.name.lower().endswith(".nfo")]
    strms = [f for f in files if f.name.lower().endswith(".strm")]
    season_dirs = [d for d in dirs if SEASON_RE.match(d.name) or SPECIALS_RE.match(d.name)]
    sig = entry_signature(items)

    def make(kind: str, stems: list[str] | None = None) -> Entry:
        mtime = ""
        for f in files:
            if f.name.lower() in ("movie.nfo", "tvshow.nfo") or f.name.lower().endswith(".strm"):
                mtime = max(mtime, f.mtime)
        return Entry(path=path, kind=kind, items=items, stems=stems or [], sig=sig, mtime=mtime)

    # nfo 与 strm 同名的才算「成对」；movie/tvshow/season.nfo 是目录级的，不算
    nfo_stems = {os.path.splitext(f.name)[0] for f in nfos}
    strm_stems = {os.path.splitext(f.name)[0] for f in strms}
    pairs = sorted((nfo_stems & strm_stems) - {"movie", "tvshow", "season"})

    # 散装目录 vs 平铺剧集：都有 tvshow.nfo 时按 stem 长相分。
    # 纪录片/NHK 288 对、纪录片/【历史影像】71 对是散装；
    # 每日更新/电视剧/TVB Viu 下的是 `01粤语` / `01国语` 这种平铺剧集。
    ep_like = [p for p in pairs if EPISODE_STEM_RE.match(p)]
    if "tvshow.nfo" in lowered:
        if season_dirs:
            return make("series")
        if pairs and len(ep_like) >= max(2, (len(pairs) * 4) // 5):
            return make("series", ep_like or pairs)
    if len(pairs) >= 2:
        return make("loose", pairs)

    if "movie.nfo" in lowered:
        if strms or allow_no_source:
            return make("movie")

    if "tvshow.nfo" in lowered:
        if season_dirs:
            return make("series")
        if strms or allow_no_source:
            # 纪录片 / 综艺里常见：剧集模板写了个 tvshow.nfo，实际就是单部片子
            return make("movie")

    base_name = os.path.basename(path.rstrip("/"))
    same_name = [f for f in nfos if os.path.splitext(f.name)[0] == base_name]
    if same_name and (strms or allow_no_source):
        return make("movie")

    if pairs:
        return make("loose", pairs)
    return None


def discover(fetcher: Fetcher, base: str, root: str, limit: int, max_depth: int = 6,
             skip: tuple[str, ...] = ()) -> list[Entry]:
    """按层 BFS 找可导入的目录。

    每层并发拉列表，并且**分块推进、够数就收手**——否则为了 6 条样本会把整站
    上千个目录都拉一遍（实测 574 次请求 vs 98 次）。
    """
    found: list[Entry] = []
    frontier: list[tuple[str, int]] = [(root, 0)]
    seen: set[str] = {root}
    chunk = max(fetcher.workers * 8, 32)
    while frontier and (limit <= 0 or len(found) < limit):
        batch, rest = frontier[:chunk], frontier[chunk:]
        urls = [join_url(base, p) for p, _d in batch]
        bodies = fetcher.fetch_dirs(urls)
        children: list[tuple[str, int]] = []
        for (path, depth), url in zip(batch, urls):
            html = bodies.get(url)
            if html is None:
                continue
            items = parse_listing(html)
            entry = classify(path, items, allow_no_source=False)
            if entry is not None:
                found.append(entry)
                continue
            if depth >= max_depth:
                continue
            for item in items:
                if not item.is_dir or item.name.startswith(".") or item.name in skip:
                    continue
                child = f"{path.rstrip('/')}/{item.name}"
                if child not in seen:
                    seen.add(child)
                    children.append((child, depth + 1))
        frontier = children + rest
    return found[:limit] if limit > 0 else found


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


# 小雅的 nfo 偶尔不合规：把两个（甚至多个）根元素直接拼在一个文件里
# （纪录片/【历史影像】 下就有两遍 <episodedetails>），严格解析会整条丢掉。
# 兜底办法：包一层假根，再挑第一个认识的根节点。
_NFO_ROOT_TAGS = ("movie", "tvshow", "episode", "episodedetails", "season",
                  "musicvideo", "album", "artist")
_XML_DECL_RE = re.compile(r"<\?xml[^>]*\?>", re.I)


def _strip_stray_declarations(xml: str) -> str:
    """包裹解析时必须把 XML 声明全去掉：声明只能出现在文档最前面，
    留在拼好的假根中间会被判成 "declaration not at start of entity"。"""
    return _XML_DECL_RE.sub("", xml)


def parse_nfo(xml: str) -> ET.Element | None:
    xml = xml.strip().lstrip("﻿")
    if not xml:
        return None
    try:
        return ET.fromstring(xml)
    except ET.ParseError:
        pass
    try:
        wrapped = ET.fromstring("<__xiaoya__>" + _strip_stray_declarations(xml) + "</__xiaoya__>")
    except ET.ParseError as exc:
        print(f"  ! NFO 解析失败: {exc}", file=sys.stderr, flush=True)
        return None
    for tag in _NFO_ROOT_TAGS:
        node = wrapped.find(tag)
        if node is not None:
            return node
    return wrapped[0] if len(wrapped) else None


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
    out: list[dict[str, str]] = []
    seen: set[tuple[str, str]] = set()
    for p in people:
        key = (p["name"], p["type"])
        if key in seen:
            continue
        seen.add(key)
        out.append(p)
    return out


def nfo_languages(node: ET.Element) -> list[str]:
    """音轨语言：tinyMediaManager 写在 <streamdetails><audio><language>。

    小雅自己生成的 nfo 里常见的是复数的 <languages>中文</languages>，两种都得认；
    一个标签里可能是「中文 / 英语」这种多值，按分隔符拆开。
    """
    langs = [(el.text or "").strip()
             for el in node.findall(".//streamdetails/audio/language") if (el.text or "").strip()]
    for tag in ("language", "languages"):
        for raw in nfo_texts(node, tag):
            langs += [x for x in re.split(r"[,/、;|]+", raw) if x]
    return uniq([x.lower() for x in langs])[:8]


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


def external_urls(providers: dict[str, str], is_series: bool) -> list[dict[str, str]]:
    """把 ProviderIds 变成客户端能点的外链（纯拼字符串，不发请求）。"""
    out: list[dict[str, str]] = []
    imdb = providers.get("imdb", "")
    tmdb = providers.get("tmdb", "")
    tvdb = providers.get("tvdb", "")
    if imdb:
        out.append({"name": "IMDb", "url": f"https://www.imdb.com/title/{imdb}/"})
    if tmdb:
        kind = "tv" if is_series else "movie"
        out.append({"name": "TheMovieDb", "url": f"https://www.themoviedb.org/{kind}/{tmdb}"})
    if tvdb:
        out.append({"name": "TheTVDB", "url": f"https://thetvdb.com/dereferrer/series/{tvdb}"})
    return out


def pick_images(base: str, path: str, items: list[DirItem], stem: str = "") -> dict[str, str]:
    """站点自带的图片（与元数据同域，比 tmdb 稳）。

    stem 给定时先找 `{stem}-poster.jpg` 这类「跟着片名走」的图（散装目录、剧集缩略图靠它），
    再退回目录级的 poster.jpg / folder.jpg。
    """
    out: dict[str, str] = {}
    lowered = {i.name.lower(): i.name for i in items if not i.is_dir}

    def add(key: str, name: str) -> None:
        if key in out:
            return
        real = lowered.get(name.lower())
        if real:
            out[key] = file_url(base, path, real)

    if stem:
        for suffix, key in STEM_IMAGE_MAP:
            for ext in IMAGE_EXTS:
                add(key, f"{stem}{suffix}.{ext}")
    for name, key in DIR_IMAGE_MAP:
        add(key, name)
    return out


def merge_images(primary: dict[str, str], fallback: dict[str, str]) -> dict[str, str]:
    merged = dict(fallback)
    merged.update(primary)
    return {k: v for k, v in merged.items() if v}


def guess_sub_lang(fname: str, default: str) -> str:
    for pattern, lang in SUB_LANG_RULES:
        if re.search(pattern, fname, re.I):
            return lang
    return default


def build_subtitles(base: str, path: str, items: list[DirItem], stem: str = "",
                    default_lang: str = "zho") -> list[dict[str, str]]:
    """字幕：整目录一部片子时全收，散装/剧集时只收跟 stem 配对的。

    配对规则：同名（`X.srt` 配 `X.mkv`），或 `{stem}.{语言}.srt` 这种带语言后缀的。
    """
    out: list[dict[str, str]] = []
    for item in items:
        if item.is_dir:
            continue
        name = item.name
        stem_no_ext, ext = os.path.splitext(name)
        if ext.lower() not in SUBTITLE_EXTS:
            continue
        if stem and stem_no_ext != stem and not stem_no_ext.startswith(stem + "."):
            continue
        out.append({
            "language": guess_sub_lang(name, default_lang),
            "title": stem_no_ext,
            "url": file_url(base, path, name),
            "codec": ext.lstrip(".").lower(),
        })
    return out


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


def apply_rewrite(link: str, rewrite: dict[str, str]) -> str:
    for old, new in rewrite.items():
        if link.startswith(old):
            return new + link[len(old):]
    return link


def sources_from_pairs(pairs: list[tuple[str, str]],
                       rewrite: dict[str, str]) -> list[dict[str, Any]]:
    """(strm 文件名, 直链) → fakemby 的 sources。"""
    out: list[dict[str, Any]] = []
    for fname, raw in pairs:
        link = apply_rewrite(raw, rewrite)
        container = os.path.splitext(urllib.parse.urlparse(link).path)[1].lstrip(".").lower()
        out.append({
            "name": source_name(fname, link),
            "url": link,
            **({"container": container} if container else {}),
        })
    return out


def strm_link_map(fetcher: Fetcher, base: str, path: str, items: list[DirItem],
                  wanted: set[str] | None = None) -> dict[str, list[tuple[str, str]]]:
    """一次性抓完目录里的 .strm，返回 stem(小写) → [(文件名, 直链)]。

    散装目录动辄几百对 nfo/strm，逐条抓会退化成「一条一个线程池」，所以这里
    一律先批量抓再本地配对。wanted 为空表示全部。
    """
    targets = [i.name for i in items
               if not i.is_dir and i.name.lower().endswith(".strm")]
    if wanted is not None:
        stems = {w.lower() for w in wanted}
        targets = [f for f in targets if os.path.splitext(f)[0].lower() in stems]
    if not targets:
        return {}
    urls = [file_url(base, path, f) for f in targets]
    bodies = fetcher.fetch_many(urls)
    out: dict[str, list[tuple[str, str]]] = {}
    for fname, url in zip(targets, urls):
        body = bodies.get(url, "")
        link = body.strip().splitlines()[0].strip() if body.strip() else ""
        if not link.lower().startswith(("http://", "https://")):
            continue
        out.setdefault(os.path.splitext(fname)[0].lower(), []).append((fname, link))
    return out


def strm_sources(fetcher: Fetcher, base: str, path: str, items: list[DirItem],
                 wanted: set[str] | None = None) -> list[dict[str, Any]]:
    """抓 .strm（每行一个直链）。wanted 为空表示全部，否则只取这些 stem。"""
    pairs: list[tuple[str, str]] = []
    for group in strm_link_map(fetcher, base, path, items, wanted).values():
        pairs.extend(group)
    return sources_from_pairs(pairs, fetcher.source_rewrite)


def common_fields(node: ET.Element, is_series: bool) -> dict[str, Any]:
    """movie.nfo / tvshow.nfo 共用的那部分。"""
    item: dict[str, Any] = {
        "overview": nfo_text(node, "plot") or nfo_text(node, "outline"),
        "genres": uniq(nfo_texts(node, "genre")),
        "tags": uniq(nfo_texts(node, "tag")),
        "studios": uniq(nfo_texts(node, "studio")),
        "countries": uniq(nfo_texts(node, "country")),
        "people": nfo_people(node),
    }
    languages = nfo_languages(node)
    if languages:
        item["languages"] = languages
    if nfo_text(node, "year").isdigit():
        item["year"] = int(nfo_text(node, "year"))
    premiered = nfo_text(node, "premiered") or nfo_text(node, "releasedate")
    if premiered:
        item["premiere_date"] = premiered[:10]
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
    urls = external_urls(providers, is_series)
    if urls:
        item["external_urls"] = urls
    return item


def movie_item(node: ET.Element, base: str, entry: Entry, stem: str,
               sources: list[dict[str, Any]], sub_lang_default: str) -> dict[str, Any]:
    """nfo 解析结果 + 已抓好的源 → 一条电影。散装目录和整目录一部片共用这里。"""
    name = nfo_text(node, "title") or (stem or os.path.basename(entry.path.rstrip("/")))
    item: dict[str, Any] = {"name": name, "type": "Movie"}
    item.update(common_fields(node, is_series=False))
    item["images"] = merge_images(
        pick_images(base, entry.path, entry.items, stem), nfo_images(node))
    item["sources"] = sources
    subs = build_subtitles(base, entry.path, entry.items, stem, sub_lang_default)
    if subs:
        item["subtitles"] = subs
    original = nfo_text(node, "originaltitle")
    if original and original != name:
        item["original_title"] = original
    if nfo_text(node, "runtime").isdigit():
        item["runtime_minutes"] = int(nfo_text(node, "runtime"))
    return item


def build_movie(fetcher: Fetcher, base: str, entry: Entry, nfo_name: str,
                stem: str = "", sub_lang_default: str = "zho") -> dict[str, Any] | None:
    node = parse_nfo(fetcher.fetch_text(file_url(base, entry.path, nfo_name)))
    if node is None:
        return None
    sources = strm_sources(fetcher, base, entry.path, entry.items, {stem} if stem else None)
    return movie_item(node, base, entry, stem, sources, sub_lang_default)


def series_header(fetcher: Fetcher, base: str, entry: Entry) -> dict[str, Any] | None:
    """tvshow.nfo → 剧集本体的字段（不含季/集）。"""
    node = parse_nfo(fetcher.fetch_text(file_url(base, entry.path, "tvshow.nfo")))
    if node is None:
        return None
    name = nfo_text(node, "title") or os.path.basename(entry.path.rstrip("/"))
    item: dict[str, Any] = {"name": name, "type": "Series"}
    item.update(common_fields(node, is_series=True))
    item["images"] = merge_images(
        pick_images(base, entry.path, entry.items), nfo_images(node))
    original = nfo_text(node, "originaltitle")
    if original and original != name:
        item["original_title"] = original
    return item


def build_series(fetcher: Fetcher, base: str, entry: Entry, max_seasons: int,
                 max_episodes: int, sub_lang_default: str = "zho") -> dict[str, Any] | None:
    item = series_header(fetcher, base, entry)
    if item is None:
        return None

    # 季：Season N / Season 0 / Specials 都算，Specials 归到第 0 季
    numbered: list[tuple[int, str]] = []
    for sub in entry.subdirs:
        m = SEASON_RE.match(sub)
        if m:
            numbered.append((int(m.group(1)), sub))
        elif SPECIALS_RE.match(sub):
            numbered.append((0, sub))
    numbered.sort()
    if max_seasons > 0:
        numbered = numbered[:max_seasons]

    season_urls = [join_url(base, f"{entry.path.rstrip('/')}/{sub}") for _n, sub in numbered]
    bodies = fetcher.fetch_dirs(season_urls)

    seasons: list[dict[str, Any]] = []
    for (season_no, sub), url in zip(numbered, season_urls):
        html = bodies.get(url)
        if html is None:
            continue
        season_path = f"{entry.path.rstrip('/')}/{sub}"
        items = parse_listing(html)
        season_img: dict[str, str] = {}
        # 季海报可能在季目录里，也可能统一放在剧集根目录（season01-poster.jpg）
        for holder, holder_path in ((items, season_path), (entry.items, entry.path)):
            for candidate in (f"season{season_no:02d}-poster.jpg", f"season{season_no}-poster.jpg"):
                hit = [i for i in holder if not i.is_dir and i.name.lower() == candidate]
                if hit:
                    season_img["Primary"] = file_url(base, holder_path, hit[0].name)
                    break
            if season_img:
                break
        if not season_img:
            season_img = pick_images(base, season_path, items)
        episodes = build_episodes(fetcher, base, season_path, items, max_episodes,
                                  sub_lang_default)
        if not episodes:
            continue
        seasons.append({
            "name": f"第 {season_no} 季" if season_no else "特别篇",
            "season_number": season_no,
            **({"images": season_img} if season_img else {}),
            "episodes": episodes,
        })
    if not seasons:
        # 没有 Season 目录：集可能直接平铺在剧目录里（小雅的 TVB Viu 就这摆法）
        flat = build_flat_episodes(fetcher, base, entry, max_episodes, sub_lang_default)
        if flat:
            seasons.append({
                "name": "第 1 季",
                "season_number": 1,
                **({"images": pick_images(base, entry.path, entry.items)} or {}),
                "episodes": flat,
            })
    if not seasons:
        return None
    item["seasons"] = seasons
    return item


def cn_number(text: str) -> int | None:
    """`十二` → 12、`二十三` → 23、`一` → 1；认不出来返回 None。"""
    digits = {"零": 0, "一": 1, "二": 2, "两": 2, "三": 3, "四": 4,
              "五": 5, "六": 6, "七": 7, "八": 8, "九": 9}
    if "十" in text:
        left, _, right = text.partition("十")
        tens = 1 if left == "" else digits.get(left)
        ones = digits.get(right, 0) if right else 0
        if tens is None or (right and right not in digits):
            return None
        return tens * 10 + ones
    total = 0
    for ch in text:
        if ch not in digits:
            return None
        total = total * 10 + digits[ch]
    return total or None


def episode_number_from_stem(stem: str) -> int | None:
    """从 stem 里抠集号：`S01E02` / `Episode 2` / `第 2 集` / `01粤语`。"""
    m = EP_FROM_NAME_RE.search(stem)
    if m:
        return int(m.group(2))
    m = re.search(r"(?i)\bep(?:isode)?\s*[._\-]?\s*(\d{1,3})", stem)
    if m:
        return int(m.group(1))
    m = re.search(r"第\s*(\d{1,4}|[一二三四五六七八九十两]+)\s*[集话話回]", stem)
    if m:
        return int(m.group(1)) if m.group(1).isdigit() else cn_number(m.group(1))
    # 纯数字开头（后面可能是中文音轨名，不能用 \b —— 中文也是单词字符）
    m = re.match(r"\s*(\d{1,3})(?!\d)", stem)
    if m:
        return int(m.group(1))
    return None


def build_flat_episodes(fetcher: Fetcher, base: str, entry: Entry, max_episodes: int,
                        sub_lang_default: str = "zho") -> list[dict[str, Any]]:
    """集平铺在剧目录里（没有 Season 目录）时的一季。

    同一集常有多条音轨（`01粤语` + `01国语`），按集号归组成**一集多条源**，
    而不是变成两集同名条目。
    """
    groups: dict[int, list[str]] = {}
    for stem in entry.stems:
        num = episode_number_from_stem(stem)
        if num is None:
            continue
        groups.setdefault(num, []).append(stem)
    if not groups:
        return []
    ordered = [groups[n] for n in sorted(groups)]
    if max_episodes > 0:
        ordered = ordered[:max_episodes]

    wanted = {s for group in ordered for s in group}
    links = strm_link_map(fetcher, base, entry.path, entry.items, wanted)
    have_nfo = {os.path.splitext(f)[0] for f in entry.files if f.lower().endswith(".nfo")}
    # 每个组挑一个真有 nfo 的 stem 作为元数据来源
    nfo_stems = [next((s for s in group if s in have_nfo), group[0]) for group in ordered]
    nfo_urls = [file_url(base, entry.path, f"{s}.nfo") for s in nfo_stems]
    bodies = fetcher.fetch_many(nfo_urls)

    episodes: list[dict[str, Any]] = []
    for num, group, nfo_stem, url in zip(sorted(groups)[:len(ordered)], ordered,
                                         nfo_stems, nfo_urls):
        node = parse_nfo(bodies.get(url, ""))
        ep: dict[str, Any] = {"name": f"第 {num} 集", "episode_number": num,
                              "season_number": 1}
        if node is not None:
            ep["name"] = nfo_text(node, "title") or ep["name"]
            overview = nfo_text(node, "plot") or nfo_text(node, "outline")
            if overview:
                ep["overview"] = overview
            if nfo_text(node, "runtime").isdigit():
                ep["runtime_minutes"] = int(nfo_text(node, "runtime"))
            aired = nfo_text(node, "aired") or nfo_text(node, "premiered")
            if aired:
                ep["premiere_date"] = aired[:10]
            providers = nfo_provider_ids(node)
            if providers:
                ep["ProviderIds"] = providers
        images = merge_images(pick_images(base, entry.path, entry.items, group[0]),
                              nfo_images(node) if node is not None else {})
        if images:
            ep["images"] = images
        srcs: list[dict[str, Any]] = []
        for stem in group:
            srcs += sources_from_pairs(links.get(stem.lower(), []), fetcher.source_rewrite)
        if srcs:
            ep["sources"] = srcs
        subs = build_subtitles(base, entry.path, entry.items, group[0], sub_lang_default)
        if subs:
            ep["subtitles"] = subs
        episodes.append(ep)
    return episodes


EP_FROM_NAME_RE = re.compile(r"(?i)s(\d{1,2})[\s._-]*e(\d{1,3})")


def build_episodes(fetcher: Fetcher, base: str, season_path: str, items: list[DirItem],
                   max_episodes: int, sub_lang_default: str = "zho") -> list[dict[str, Any]]:
    # season.nfo 是「季」自己的元数据，不是一集，别混进来
    nfos = sorted(i.name for i in items
                  if not i.is_dir and i.name.lower().endswith(".nfo")
                  and i.name.lower() != "season.nfo")
    if max_episodes > 0:
        nfos = nfos[:max_episodes]
    strms = [i.name for i in items if not i.is_dir and i.name.lower().endswith(".strm")]
    # 整季的直链一次抓完，别每集各起一批请求
    links = strm_link_map(fetcher, base, season_path, items)

    def sources_for(stem: str) -> list[dict[str, Any]]:
        return sources_from_pairs(links.get(stem.lower(), []), fetcher.source_rewrite)

    episodes: list[dict[str, Any]] = []
    if nfos:
        urls = [file_url(base, season_path, f) for f in nfos]
        bodies = fetcher.fetch_many(urls)
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
                ep["premiere_date"] = aired[:10]
            images = merge_images(pick_images(base, season_path, items, stem), nfo_images(node))
            if images:
                ep["images"] = images
            providers = nfo_provider_ids(node)
            if providers:
                ep["ProviderIds"] = providers
            srcs = sources_for(stem)
            if srcs:
                ep["sources"] = srcs
            subs = build_subtitles(base, season_path, items, stem, sub_lang_default)
            if subs:
                ep["subtitles"] = subs
            episodes.append(ep)
    else:
        # 没写 nfo 的季（只有一堆 strm）：从文件名 SxxExx 里抠集号，别整季丢掉
        for fname in strms:
            stem = os.path.splitext(fname)[0]
            m = EP_FROM_NAME_RE.search(stem)
            if not m:
                continue
            srcs = sources_for(stem)
            if not srcs:
                continue
            ep = {
                "name": re.sub(r"(?i)^.*?s\d{1,2}e\d{1,3}[\s._-]*", "", stem) or stem,
                "season_number": int(m.group(1)),
                "episode_number": int(m.group(2)),
                "sources": srcs,
            }
            images = pick_images(base, season_path, items, stem)
            if images:
                ep["images"] = images
            subs = build_subtitles(base, season_path, items, stem, sub_lang_default)
            if subs:
                ep["subtitles"] = subs
            episodes.append(ep)
    episodes.sort(key=lambda e: (e.get("episode_number", 0)))
    return episodes


def build_entry_item(fetcher: Fetcher, base: str, entry: Entry, max_seasons: int,
                     max_episodes: int, sub_lang_default: str) -> list[dict[str, Any]]:
    """一个 Entry 可能产出多条（散装目录）。"""
    if entry.kind == "series":
        item = build_series(fetcher, base, entry, max_seasons, max_episodes, sub_lang_default)
        return [item] if item else []
    if entry.kind == "loose":
        # 散装目录：nfo 和 strm 各自批量抓一次，再按 stem 本地配对
        nfo_urls = [file_url(base, entry.path, f"{stem}.nfo") for stem in entry.stems]
        bodies = fetcher.fetch_many(nfo_urls)
        links = strm_link_map(fetcher, base, entry.path, entry.items)
        out: list[dict[str, Any]] = []
        for stem, url in zip(entry.stems, nfo_urls):
            node = parse_nfo(bodies.get(url, ""))
            if node is None:
                continue
            sources = sources_from_pairs(links.get(stem.lower(), []), fetcher.source_rewrite)
            out.append(movie_item(node, base, entry, stem, sources, sub_lang_default))
        return out
    nfo_name = "movie.nfo"
    files = {f.lower() for f in entry.files}
    if "movie.nfo" not in files and "tvshow.nfo" in files:
        nfo_name = "tvshow.nfo"
    elif "movie.nfo" not in files:
        base_name = os.path.basename(entry.path.rstrip("/"))
        if f"{base_name}.nfo".lower() in files:
            nfo_name = f"{base_name}.nfo"
    item = build_movie(fetcher, base, entry, nfo_name, "", sub_lang_default)
    return [item] if item else []


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
    log(f"  直链体检（{len(urls)} 条）：")
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
        log(f"    {status} {note} {url[:70]}")


# --------------------------------------------------------------------------- #
# 断点续传 / 增量：state + 条目仓库
# --------------------------------------------------------------------------- #
def entry_key(entry: Entry) -> str:
    """state 的键。散装目录一个目录多条，键要带上 stem。"""
    if entry.kind == "loose":
        return f"{entry.path}#loose"
    return entry.path


class Store:
    """进度仓库：state.json 记状态，items/<cat>.jsonl 存构建结果（追加写，后写的赢）。"""

    def __init__(self, state_path: str, items_dir: str):
        self.state_path = state_path
        self.items_dir = items_dir
        self.data: dict[str, Any] = {"version": STATE_VERSION, "updated_at": "", "categories": {}}
        self._dirty = False
        if os.path.exists(state_path):
            try:
                with open(state_path, "r", encoding="utf-8") as fh:
                    loaded = json.load(fh)
                if isinstance(loaded, dict) and loaded.get("version") == STATE_VERSION:
                    self.data = loaded
            except (json.JSONDecodeError, OSError) as exc:
                print(f"! state 读不出来，从头开始：{exc}", file=sys.stderr, flush=True)

    # ---- state ----
    def cat(self, name: str) -> dict[str, Any]:
        cats = self.data.setdefault("categories", {})
        rec = cats.get(name)
        if not isinstance(rec, dict):
            rec = {"root": "", "library": "", "scanned_at": "", "entries": {}}
            cats[name] = rec
        rec.setdefault("entries", {})
        return rec

    def save(self) -> None:
        os.makedirs(os.path.dirname(os.path.abspath(self.state_path)), exist_ok=True)
        self.data["updated_at"] = now_iso()
        blob = json.dumps(self.data, ensure_ascii=False)
        tmp = self.state_path + ".tmp"
        try:
            with open(tmp, "w", encoding="utf-8") as fh:
                fh.write(blob)
            os.replace(tmp, self.state_path)  # 原子替换：写一半崩了也不会毁掉旧 state
        except OSError as exc:
            # Windows 上 os.replace 会被杀毒/索引占用挡一下（WinError 5）。
            # 进度存盘比"原子"重要得多，退回直接覆盖写。
            print(f"! state 原子替换失败（{exc}），改为直接写入", file=sys.stderr, flush=True)
            try:
                with open(self.state_path, "w", encoding="utf-8") as fh:
                    fh.write(blob)
            except OSError as exc2:
                print(f"! state 写盘失败：{exc2}", file=sys.stderr, flush=True)
        self._dirty = False

    # ---- 条目仓库 ----
    def items_path(self, name: str) -> str:
        return os.path.join(self.items_dir, f"{name}.jsonl")

    def append_items(self, name: str, rows: list[dict[str, Any]]) -> None:
        if not rows:
            return
        os.makedirs(self.items_dir, exist_ok=True)
        with open(self.items_path(name), "a", encoding="utf-8") as fh:
            for row in rows:
                fh.write(json.dumps(row, ensure_ascii=False) + "\n")

    def load_items(self, name: str) -> dict[str, dict[str, Any]]:
        """读回该分类的全部构建结果，同 key（一个目录）后写的覆盖先写的。

        一行 = 一个目录，行里是该目录产出的**全部**条目：散装目录一次能出几百部片，
        早先按「一行一条」存会被 dict 覆盖成只剩最后一条。
        """
        out: dict[str, dict[str, Any]] = {}
        path = self.items_path(name)
        if not os.path.exists(path):
            return out
        with open(path, "r", encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                key = row.get("key") or row.get("path")
                if key:
                    out[key] = row
        return out

    def compact(self, name: str) -> None:
        rows = self.load_items(name)
        path = self.items_path(name)
        os.makedirs(self.items_dir, exist_ok=True)
        tmp = path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as fh:
            for row in rows.values():
                fh.write(json.dumps(row, ensure_ascii=False) + "\n")
        os.replace(tmp, path)


# --------------------------------------------------------------------------- #
# 构建 / 推送
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
        with urllib.request.urlopen(req, timeout=120) as resp:
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


def ensure_library(server: str, api_key: str, name: str, lib_type: str) -> None:
    code, body = api_json(server, api_key, "GET", "/api/admin/libraries")
    if code == 200 and isinstance(body, list):
        for lib in body:
            # 库对象直出 database.Library，字段名是大写 Name/Type
            if (lib.get("name") or lib.get("Name")) == name:
                log(f"  库已存在：{name}（{lib.get('type') or lib.get('Type')}）")
                return
    code, body = api_json(server, api_key, "POST", "/api/admin/libraries",
                          {"name": name, "type": lib_type, "sort_order": 0})
    if code in (200, 201):
        log(f"  建库：{name}（{lib_type}）")
    else:
        print(f"  ! 建库失败 {code}: {body}", file=sys.stderr, flush=True)


def count_nodes(item: dict[str, Any]) -> int:
    total = 1
    for season in item.get("seasons", []):
        total += 1 + len(season.get("episodes", []))
    return total


def row_items(row: dict[str, Any]) -> list[dict[str, Any]]:
    """一行的条目列表；兼容旧格式（一行一条、字段叫 item）。"""
    items = row.get("items")
    if isinstance(items, list):
        return [i for i in items if isinstance(i, dict)]
    legacy = row.get("item")
    return [legacy] if isinstance(legacy, dict) else []


def chunk_items(rows: list[dict[str, Any]], max_items: int = 200,
                max_nodes: int = 3000, max_bytes: int = 6 * 1024 * 1024) -> list[list[tuple]]:
    """按 fakemby 的上限切批：1000 items / 5000 nodes / 8MB 请求体。

    返回一批 `[(所属目录行, 条目)]`。批的边界按「条目」算，但推送成功后要按「目录」
    记 pushed_sig（一个散装目录几百部片可能被切开），所以两个都得留着。
    """
    batches: list[list[tuple]] = []
    cur: list[tuple] = []
    cur_nodes = 0
    cur_bytes = 0
    for row in rows:
        for item in row_items(row):
            nodes = count_nodes(item)
            size = len(json.dumps(item, ensure_ascii=False).encode("utf-8"))
            if cur and (len(cur) + 1 > max_items or cur_nodes + nodes > max_nodes
                        or cur_bytes + size > max_bytes):
                batches.append(cur)
                cur, cur_nodes, cur_bytes = [], 0, 0
            cur.append((row, item))
            cur_nodes += nodes
            cur_bytes += size
    if cur:
        batches.append(cur)
    return batches


def push_rows(server: str, api_key: str, library: str, lib_type: str,
              rows: list[dict[str, Any]], dry_run: bool,
              on_batch_done) -> tuple[int, int]:
    """推一批，返回 (成功条目数, 失败条目数)。"""
    ok = failed = 0
    if not rows:
        return ok, failed
    if not dry_run:
        ensure_library(server, api_key, library, lib_type)
    for idx, batch in enumerate(chunk_items(rows), 1):
        body = {"library": library, "items": [item for _row, item in batch]}
        size = len(json.dumps(body, ensure_ascii=False).encode("utf-8"))
        log(f"  批次 {idx}: {len(batch)} 条 / {size // 1024} KB")
        if dry_run:
            ok += len(batch)
            continue
        code, resp = api_json(server, api_key, "POST", "/api/admin/import", body)
        summary = json.dumps(resp, ensure_ascii=False)[:300]
        log(f"    -> HTTP {code}: {summary}")
        if code != 200:
            failed += len(batch)
            continue
        errors = resp.get("errors") if isinstance(resp, dict) else None
        if errors:
            # 批次里只有部分条目失败：按返回顺序无法精确对应，整批标记为待重试
            failed += len(errors)
            ok += max(len(batch) - len(errors), 0)
            continue
        ok += len(batch)
        on_batch_done([row for row, _item in batch])
    return ok, failed


# --------------------------------------------------------------------------- #
# 命令
# --------------------------------------------------------------------------- #
def make_fetcher(args: argparse.Namespace) -> Fetcher:
    return Fetcher(args.cache_dir, args.timeout, args.retries, args.workers,
                   source_rewrite=args.source_rewrite,
                   dir_ttl=args.cache_ttl if args.cache_ttl > 0 else None)


def scan_category(args: argparse.Namespace, fetcher: Fetcher, category: str) -> list[Entry]:
    rel, lib_type, default_name = CATEGORIES[category]
    log(f"[{category}] {rel}（limit={args.limit}）")
    entries = discover(fetcher, args.base_url, rel, args.limit, args.max_depth, tuple(args.skip))
    return entries


def cmd_scan(args: argparse.Namespace) -> int:
    fetcher = make_fetcher(args)
    store = Store(args.state, args.items_dir)
    for category in args.category:
        entries = scan_category(args, fetcher, category)
        for entry in entries:
            extra = f"（{len(entry.stems)} 条）" if entry.kind == "loose" else ""
            log(f"  {entry.kind:<6} {entry.path}{extra}")
        cat = store.cat(category)
        cat["root"] = CATEGORIES[category][0]
        cat["library"] = args.library or CATEGORIES[category][2]
        cat["scanned_at"] = now_iso()
        log(f"  小计 {len(entries)} 个目录；缓存命中 {fetcher.stats['hit']} / "
            f"抓取 {fetcher.stats['miss']} / 失败 {fetcher.stats['fail']}")
    store.save()
    return 0


def build_category(args: argparse.Namespace, fetcher: Fetcher, store: Store,
                   category: str) -> tuple[list[dict[str, Any]], dict[str, int]]:
    """扫描 + 构建（带断点续传与增量），返回 (待处理的行, 统计)。"""
    rel, lib_type, default_name = CATEGORIES[category]
    log(f"[{category}] {rel}（limit={args.limit}）")
    entries = discover(fetcher, args.base_url, rel, args.limit, args.max_depth, tuple(args.skip))
    if args.offset:
        entries = entries[args.offset:]
    cat = store.cat(category)
    cat["root"] = rel
    cat["library"] = args.library or default_name
    cat["scanned_at"] = now_iso()

    since_dt = None
    if args.since_days > 0:
        since_dt = dt.datetime.now() - dt.timedelta(days=args.since_days)

    known = cat["entries"]
    todo: list[Entry] = []
    reused = 0
    deferred = 0
    for entry in entries:
        key = entry_key(entry)
        rec = known.get(key)
        unchanged = (isinstance(rec, dict) and rec.get("status") == "done"
                     and rec.get("sig") == entry.sig)
        if unchanged and not args.full:
            # 签名没变 —— 增量就靠这一步省掉绝大部分网络请求
            reused += 1
            continue
        if rec is not None and since_dt is not None and entry.mtime:
            # 已知条目的改动比 --since-days 还早：这次先不跟，留在 state 里下次再说。
            # 新目录（rec 为空）不受限制，否则增量永远发现不了新片。
            stamp = parse_mtime(entry.mtime)
            if stamp and stamp < since_dt:
                deferred += 1
                continue
        todo.append(entry)
    log(f"  发现 {len(entries)} 个目录：复用 {reused}，待处理 {len(todo)}，"
        f"按时间推迟 {deferred}")

    rows: list[dict[str, Any]] = []          # 待落盘的缓冲
    built: list[dict[str, Any]] = []         # 本次构建出来的全部（不管有没有落盘）
    stats = {"built": 0, "failed": 0, "skipped": 0, "items": 0}
    pending_flush = 0
    for idx, entry in enumerate(todo, 1):
        key = entry_key(entry)
        rec = known.get(key)
        if isinstance(rec, dict) and rec.get("status") == "failed" and rec.get("sig") == entry.sig \
                and not args.retry_failed:
            stats["skipped"] += 1
            continue
        try:
            items = build_entry_item(fetcher, args.base_url, entry, args.max_seasons,
                                     args.max_episodes, args.sub_lang_default)
        except Exception as exc:  # noqa: BLE001
            items = []
            print(f"  ! 条目构建失败 {entry.path}: {exc}", file=sys.stderr, flush=True)
        if not items:
            known[key] = {"path": entry.path, "kind": entry.kind, "sig": entry.sig,
                          "status": "failed", "error": "无可用条目（缺 nfo 或直链）",
                          "updated_at": now_iso(), "pushed_sig": ""}
            stats["failed"] += 1
        else:
            # 一个目录一行：散装目录里的几百部片必须一起存，按条存会互相覆盖
            rows.append({"key": key, "path": entry.path, "sig": entry.sig,
                         "items": items})
            built.append(rows[-1])
            known[key] = {"path": entry.path, "kind": entry.kind, "sig": entry.sig,
                          "status": "done", "error": "", "updated_at": now_iso(),
                          "pushed_sig": (rec or {}).get("pushed_sig", ""),
                          "name": items[0].get("name", "")}
            stats["built"] += 1
            stats["items"] += len(items)
            if entry.kind == "loose" and len(items) > 1:
                # 散装目录里是一堆片子，打首条片名会让人以为只出了一条
                label = (os.path.basename(entry.path.rstrip("/")) or entry.path)
                label = f"{label}（散装 {len(items)} 部）"
            else:
                label = items[0].get("name", "") or entry.path
            log(f"  [{idx}/{len(todo)}] {items[0]['type']:<6} {label}")
        pending_flush += 1
        if pending_flush >= max(args.flush_every, 1):
            # 断点续传的关键：边做边存，Ctrl+C 或崩了都不丢进度
            store.append_items(category, rows)
            rows = []
            store.save()
            pending_flush = 0
    store.append_items(category, rows)
    store.save()
    log(f"  构建完成：新增/更新 {stats['built']} 个目录（{stats['items']} 条），"
        f"失败 {stats['failed']}，跳过 {stats['skipped']}")

    # 本次要推的 = 新构建的 + 之前构建过但没推成功的
    if args.push_all:
        all_rows = list(store.load_items(category).values())
    else:
        all_rows = list(built)
        for key, rec in known.items():
            if rec.get("status") == "done" and rec.get("pushed_sig") != rec.get("sig"):
                if not any(r["key"] == key for r in all_rows):
                    all_rows.append({"key": key, "path": rec.get("path", ""),
                                     "sig": rec.get("sig", ""), "items": []})
        loaded = store.load_items(category)
        resolved: list[dict[str, Any]] = []
        for row in all_rows:
            if not row_items(row):
                cached = loaded.get(row["key"])
                if cached and cached.get("sig") == row.get("sig") and row_items(cached):
                    resolved.append(cached)
            else:
                resolved.append(row)
        all_rows = resolved
    if args.compact:
        store.compact(category)
    return all_rows, stats


def cmd_build(args: argparse.Namespace) -> int:
    fetcher = make_fetcher(args)
    store = Store(args.state, args.items_dir)
    for category in args.category:
        rows, _stats = build_category(args, fetcher, store, category)
        items = [i for r in rows for i in row_items(r)]
        payload = {"library": store.cat(category)["library"], "items": items}
        path = args.out
        if len(args.category) > 1 and path:
            path = path[:-5] + f"_{category}.json" if path.endswith(".json") else f"{path}_{category}.json"
        if path:
            os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                json.dump(payload, fh, ensure_ascii=False, indent=2)
            log(f"  写出 {path}（{len(items)} 条）")
        if args.check_sources:
            probe_sources(items, args.check_sources)
    return 0


def cmd_push(args: argparse.Namespace) -> int:
    key = args.api_key or os.environ.get("FAKEMBY_ADMIN_API_KEY", "")
    store = Store(args.state, args.items_dir)
    if args.file:
        with open(args.file, "r", encoding="utf-8") as fh:
            payload = json.load(fh)
        if args.library:
            payload["library"] = args.library
        if not payload.get("library"):
            print("缺少库名：用 --library 指定，或在 JSON 里带 library 字段", file=sys.stderr)
            return 2
        items = payload.get("items", [])
        lib_type = payload.get("library_type") or (
            "tvshows" if any(i.get("type") == "Series" for i in items) else "movies")
        log(f"推送 {args.file} → {args.server} 库[{payload['library']}]")
        rows = [{"key": f"file:{i}", "path": "", "sig": "", "items": [item]}
                for i, item in enumerate(items)]
        ok, failed = push_rows(args.server, key, payload["library"], lib_type, rows,
                               args.dry_run, lambda _batch: None)
        log(f"  完成：成功 {ok}，失败 {failed}")
        return 0 if failed == 0 else 1

    # 从 state 推：只推没推过 / 更新过的
    total_ok = total_failed = 0
    for category in args.category:
        cat = store.cat(category)
        library = args.library or cat.get("library") or CATEGORIES[category][2]
        lib_type = CATEGORIES[category][1]
        loaded = store.load_items(category)
        pending: list[dict[str, Any]] = []
        for entry_key_, rec in cat.get("entries", {}).items():
            if rec.get("status") != "done":
                continue
            if not args.push_all and rec.get("pushed_sig") == rec.get("sig"):
                continue
            row = loaded.get(entry_key_)
            if row and row_items(row):
                pending.append(row)
        log(f"[{category}] 库[{library}] 待推送 {len(pending)} 个目录")
        if not pending:
            continue
        if args.check_sources:
            probe_sources([i for r in pending for i in row_items(r)], args.check_sources)

        def done(batch: list[dict[str, Any]], _cat=category) -> None:
            for row in batch:
                entry = store.cat(_cat)["entries"].get(row["key"])
                if isinstance(entry, dict):
                    entry["pushed_sig"] = row.get("sig", "")
                    entry["pushed_at"] = now_iso()
            store.save()

        ok, failed = push_rows(args.server, key, library, lib_type, pending, args.dry_run, done)
        total_ok += ok
        total_failed += failed
        store.save()
    log(f"推送完成：成功 {total_ok}，失败 {total_failed}")
    return 0 if total_failed == 0 else 1


def cmd_run(args: argparse.Namespace) -> int:
    key = args.api_key or os.environ.get("FAKEMBY_ADMIN_API_KEY", "")
    if not key and not args.dry_run:
        print("缺少管理密钥：--api-key 或 FAKEMBY_ADMIN_API_KEY", file=sys.stderr)
        return 2
    fetcher = make_fetcher(args)
    store = Store(args.state, args.items_dir)
    for category in args.category:
        rows, _stats = build_category(args, fetcher, store, category)
        items = [i for r in rows for i in row_items(r)]
        if args.out:
            path = args.out
            if len(args.category) > 1:
                path = path[:-5] + f"_{category}.json" if path.endswith(".json") else f"{path}_{category}.json"
            os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                json.dump({"library": store.cat(category)["library"], "items": items},
                          fh, ensure_ascii=False, indent=2)
            log(f"  写出 {path}")
        if args.check_sources:
            probe_sources(items, args.check_sources)
        library = args.library or store.cat(category)["library"]
        log(f"推送 [{library}] {len(items)} 条")

        def done(batch: list[dict[str, Any]], _cat=category) -> None:
            for row in batch:
                entry = store.cat(_cat)["entries"].get(row["key"])
                if isinstance(entry, dict):
                    entry["pushed_sig"] = row.get("sig", "")
                    entry["pushed_at"] = now_iso()
            store.save()

        ok, failed = push_rows(args.server, key, library, CATEGORIES[category][1], rows,
                               args.dry_run, done)
        store.save()
        log(f"  [{category}] 成功 {ok}，失败 {failed}")
    return 0


def cmd_status(args: argparse.Namespace) -> int:
    store = Store(args.state, args.items_dir)
    if not store.data.get("categories"):
        log("还没有进度：先跑 scan 或 build")
        return 0
    log(f"state: {args.state}")
    log(f"条目仓库: {args.items_dir}")
    for name, cat in store.data["categories"].items():
        entries = cat.get("entries", {})
        done = sum(1 for r in entries.values() if r.get("status") == "done")
        failed = sum(1 for r in entries.values() if r.get("status") == "failed")
        pushed = sum(1 for r in entries.values()
                     if r.get("status") == "done" and r.get("pushed_sig") == r.get("sig"))
        log(f"  [{name}] {cat.get('root', '')} → 库[{cat.get('library', '')}] "
            f"扫描于 {cat.get('scanned_at', '-')}")
        log(f"      共 {len(entries)}：已构建 {done}，失败 {failed}，"
            f"已推送 {pushed}，待推送 {done - pushed}")
    return 0


def cmd_reset(args: argparse.Namespace) -> int:
    store = Store(args.state, args.items_dir)
    for category in args.category:
        store.data.setdefault("categories", {}).pop(category, None)
        path = store.items_path(category)
        if os.path.exists(path):
            os.remove(path)
        log(f"  已清空 {category}")
    store.save()
    return 0


def main(argv: list[str] | None = None) -> int:
    # Windows 控制台可能是 GBK，中文片名一律按 UTF-8 输出，乱码也比抛异常强
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8", errors="replace")  # type: ignore[attr-defined]
        except Exception:  # noqa: BLE001
            pass
    parser = argparse.ArgumentParser(description="小雅 每日更新 → FakEmby 导入工具")
    parser.add_argument("command", choices=("scan", "build", "push", "run", "status", "reset"))
    parser.add_argument("--base-url", default=DEFAULT_BASE)
    parser.add_argument("--category", default="movie",
                        help="逗号分隔：movie,tv,anime,anime_movie,lib_movie,lib_tv,"
                             "lib_anime,doc,doc_scraped,variety；daily=每日更新四项；all=全部")
    parser.add_argument("--limit", type=int, default=10, help="每个分类抓几个目录，0=不限")
    parser.add_argument("--offset", type=int, default=0, help="跳过前 N 个目录（分批用）")
    parser.add_argument("--max-depth", type=int, default=6)
    parser.add_argument("--max-seasons", type=int, default=0, help="每部剧最多几季，0=不限")
    parser.add_argument("--max-episodes", type=int, default=0, help="每季最多几集，0=不限")
    parser.add_argument("--skip", default="", help="跳过的目录名，逗号分隔")
    parser.add_argument("--library", default="", help="覆盖默认库名")
    parser.add_argument("--out", default="", help="输出 JSON 路径")
    parser.add_argument("--file", default="", help="push 时读取的 JSON（不给则从 state 推）")
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
    parser.add_argument("--cache-ttl", type=int, default=0,
                        help="目录列表缓存保鲜秒数，日常增量建议 86400（0=不刷新）")
    parser.add_argument("--timeout", type=float, default=20.0)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--state", default=os.path.join("dist", "xiaoya", "state.json"),
                        help="断点续传的进度文件")
    parser.add_argument("--items-dir", default=os.path.join("dist", "xiaoya", "items"),
                        help="构建结果（JSONL）目录")
    parser.add_argument("--full", action="store_true", help="忽略缓存，全部重新构建")
    parser.add_argument("--push-all", action="store_true", help="忽略 pushed_sig，全量重推")
    parser.add_argument("--since-days", type=int, default=0,
                        help="只处理最近 N 天动过的条目（0=不限）")
    parser.add_argument("--retry-failed", action="store_true", help="重试上次失败的条目")
    parser.add_argument("--flush-every", type=int, default=5,
                        help="每处理几个目录存一次盘（断点续传粒度）")
    parser.add_argument("--compact", action="store_true", help="构建后顺手压缩 JSONL 去重")
    parser.add_argument("--sub-lang-default", default="zho",
                        help="字幕文件名里认不出语言时的默认值")
    args = parser.parse_args(argv)

    if args.category == "all":
        args.category = list(CATEGORIES)
    elif args.category == "daily":
        args.category = list(DAILY_CATEGORIES)
    else:
        args.category = [c.strip() for c in args.category.split(",") if c.strip()]
    for cat in args.category:
        if cat not in CATEGORIES:
            parser.error(f"未知分类 {cat}，可选：{','.join(CATEGORIES)} 或 daily/all")
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

    handler = {"scan": cmd_scan, "build": cmd_build, "push": cmd_push,
               "run": cmd_run, "status": cmd_status, "reset": cmd_reset}[args.command]
    try:
        return handler(args)
    except KeyboardInterrupt:
        print("\n! 收到中断，进度已存盘（下次跑会从断点继续）", file=sys.stderr, flush=True)
        return 130


if __name__ == "__main__":
    sys.exit(main())
