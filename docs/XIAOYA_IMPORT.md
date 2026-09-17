# 从小雅（emby.xiaoya.pro）导入元数据

`scripts/tools/xiaoya_import.py` 把小雅「每日更新」目录里现成的 Emby 元数据（NFO + 图片 + strm 直链）
翻译成 FakEmby 的 `/api/admin/import` 请求。只做搬运，不抓任何在线元数据，也不需要转码。

只依赖 Python 标准库，3.9+ 可跑。

## 小雅那边长什么样

站点是 nginx 目录列表，结构是：

```
每日更新/电影/{地区}/{片名 (年份)}/      movie.nfo、poster.jpg、*.strm
每日更新/电视剧/{地区}/{剧名 (年份)}/    tvshow.nfo、poster/fanart.jpg、season01-poster.jpg、Season N/
每日更新/动漫/{日本|国漫|美漫|其它}/{年份}/{番名}/   同上（结构同电视剧）
Season N/                              每集一对同名 .nfo / .strm
```

- `.nfo`：tinyMediaManager 写的 Kodi/Emby 风格 XML，标题、年份、简介、类型、演职员、评分、IMDb/TMDB ID 全在里面。
- `.strm`：纯文本，只有一行播放直链（形如 `http://xiaoya.host:5678/d/...`）。
- 图片：优先用目录里自带的（跟站点同域，国内直连稳）；目录里没有才退回 nfo 里的 `image.tmdb.org` 图。

三个根目录下都有个 `115/` 子目录，是网盘镜像版，和地区目录下的内容**有重叠**。同一个库里重复导入会按名称/ProviderIds 命中已有条目变成更新，不会变出双份，但会白跑一遍。

## 用法

四个子命令，参数通用：

| 参数 | 说明 |
| --- | --- |
| `--base-url` | 站点根，默认 `https://emby.xiaoya.pro` |
| `--category` | `movie` / `tv` / `anime`，逗号分隔或 `all` |
| `--limit` | 每个分类抓几条 |
| `--offset` | 跳过前 N 条，配合 limit 分批跑 |
| `--skip` | 跳过的目录名，逗号分隔（例：`--skip 115`） |
| `--max-seasons` / `--max-episodes` | 控制剧集抓取规模，0=不限 |
| `--out` / `--file` | build 输出 / push 输入的 JSON 路径 |
| `--server` / `--api-key` | 推送目标，api-key 也可走 `FAKEMBY_ADMIN_API_KEY` |
| `--source-url-rewrite OLD=NEW` | 直链前缀重写，可多次 |
| `--check-sources N` | 导入前 HEAD 抽查 N 条直链 |
| `--cache-dir` / `--no-cache` | 抓取缓存（默认 `dist/cache/xiaoya`，重复跑靠它秒回） |
| `--workers` / `--timeout` / `--retries` | 并发与容错 |

### 1）先看能扫到什么（只列目录，不解析 NFO）

```bash
python scripts/tools/xiaoya_import.py scan --category movie --limit 30
```

### 2）抓下来生成导入 JSON

```bash
python scripts/tools/xiaoya_import.py build --category movie --limit 10 --skip 115 --out dist/xiaoya/movie.json
```

### 3）推给 FakEmby（自动建库、自动分批）

```bash
python scripts/tools/xiaoya_import.py push --file dist/xiaoya/movie.json \
    --server http://localhost:8096 --api-key <管理密钥>
```

库类型按分类定：电影 → `movies`，电视剧/动漫 → `tvshows`。库已存在就跳过建库。

### 4）一把梭（抓完直接推）

```bash
python scripts/tools/xiaoya_import.py run --category all --limit 10 \
    --max-seasons 3 --max-episodes 8 \
    --server http://localhost:8096 --api-key <管理密钥> \
    --out dist/xiaoya/all
```

## 全量导入要注意

1. **先小批量试再放开**。`--limit 10 --max-seasons 2 --max-episodes 5` 跑一轮，看库里对不对，再去掉限制。
2. **分批靠 `--limit` + `--offset`**，别一次全上：一个批次的请求体上限 8 MB、1000 个顶层条目、5000 个节点（工具已按这个切批）。
3. **剧集的抓取量很大**。一季几十集，每集要取一个 nfo + 一个 strm，全量是几千次请求；缓存开着的话重跑很快。
4. **直链能不能播不由本工具保证**。strm 里写的是小雅自己的 alist 地址（`xiaoya.host:5678`），导入时不会去连。想换成自己的 alist：

   ```bash
   --source-url-rewrite http://xiaoya.host:5678=http://192.168.1.5:5678
   ```

   导入前想体检一下就用 `--check-sources 5`，它会 HEAD 几条直链并打印状态码。
5. **图片**：目录自带图优先；退回 `image.tmdb.org` 时，客户端所在网络得能访问，否则海报空白。
6. **重复导入是幂等的**——同一库里按名称/ProviderIds 命中已有条目，只会更新，不会翻倍。

## 实测（2026-09-18）

电影 10 部 + 电视剧 10 部（13 季 104 集）+ 动漫 10 部（19 季 145 集），共 311 个节点、259 条播放源，
全部带图片，导入后 `/emby/Items/Counts` 显示 Movie 10 / Series 20 / Season 32 / Episode 249。

站点图片与 tmdb 图片均可达；`xiaoya.host:5678` 从本机连不上（直连重置、走代理 502），
所以只验证了元数据层，播放需要换成可达的 alist 地址后再试。
