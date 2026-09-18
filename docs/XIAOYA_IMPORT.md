# 从小雅（emby.xiaoya.pro）导入元数据

`scripts/tools/xiaoya_import.py` 把小雅站点上现成的 Emby 元数据（NFO + 图片 + strm 直链）
翻译成 FakEmby 的 `/api/admin/import` 请求。只做搬运，不抓任何在线元数据，也不需要转码。

只依赖 Python 标准库，3.9+ 可跑。

- **覆盖面**：站点 8 个顶层目录里能导入的 10 个分类（见下表）
- **断点续传**：进度写盘，中断后重跑接着来
- **日常增量**：按目录内容签名 + 修改时间判断，只处理变过的

## 小雅那边长什么样

站点是 nginx 目录列表。顶层有 8 个目录：

```
每日更新/   电影/  电视剧/  纪录片/  纪录片（已刮削）/  综艺/  音乐/
           📺画质演示测试（4K，8K，HDR，Dolby）/
```

工具内置 10 个分类（`--category`）：

| 分类 | 站点目录 | 库类型 | 默认库名 |
| --- | --- | --- | --- |
| `movie` | 每日更新/电影 | movies | 电影 |
| `tv` | 每日更新/电视剧 | tvshows | 电视剧 |
| `anime` | 每日更新/动漫 | tvshows | 动漫 |
| `anime_movie` | 每日更新/动漫剧场版 | movies | 动漫剧场版 |
| `lib_movie` | 电影 | movies | 电影库 |
| `lib_tv` | 电视剧 | tvshows | 电视剧库 |
| `lib_anime` | 动漫 | tvshows | 动漫库 |
| `doc` | 纪录片 | movies | 纪录片 |
| `doc_scraped` | 纪录片（已刮削） | movies | 纪录片（已刮削） |
| `variety` | 综艺 | tvshows | 综艺 |

`daily` = 前四项，`all` = 全部十项。

不进表的两个：`音乐/`（FakEmby 的导入类型只认 Movie/Series/Season/Episode，音频导不了）、
`📺画质演示测试/`（演示片源，不是正常节目）。

### 目录的五种摆法

| 摆法 | 长相 | 判定 |
| --- | --- | --- |
| 整目录一部电影 | `movie.nfo` + 若干 `.strm` | 有 movie.nfo |
| 整目录一部剧 | `tvshow.nfo` + `Season N/` | 有 tvshow.nfo 且有季目录 |
| **集平铺在剧目录** | `tvshow.nfo` + `01粤语.nfo/strm`、`01国语.strm`…（无 Season 目录） | 有 tvshow.nfo 且配对 stem 大多「像一集」 |
| 目录名即片名 | `一家之主 (2022)/一家之主 (2022).nfo` | 单条 nfo/strm 同名 |
| **散装目录** | 一个目录平铺几十上百部片 | nfo 与 strm 同名配对 ≥ 2 组，stem 是完整片名 |

散装目录很常见（`电影/人人影视电影合集` 678 部、`纪录片/NHK` 288 部、
`纪录片/【历史影像】` 71 部），按文件名主干（stem）配对 nfo 与 strm，一条一对。

「剧集」与「散装」都有 tvshow.nfo 时按 stem 长相区分：`01粤语` 是一集，
`【历史影像】120年前的清朝` 是一部片。平铺剧集里同一集常有多条音轨
（`01粤语` + `01国语`），会按集号归组成**一集多条源**，而不是两集同名条目。

### 文件里有什么

- `.nfo`：tinyMediaManager 写的 Kodi/Emby 风格 XML。标题、年份、简介、类型、标签、演职员、
  评分、分级、时长、首播日期、制片方、国家/地区、`ProviderIds` 都在里面。
  （少数 nfo 不合规——把两个根元素直接拼在一个文件里，工具会包一层假根取第一个来救。）
- `.strm`：纯文本，只有一行播放直链（形如 `http://xiaoya.host:5678/d/...`）。
- 图片：两套命名。目录级 `poster.jpg` / `folder.jpg` / `fanart.jpg`；跟片名走的
  `{stem}-poster.jpg`、`{stem}-thumb.jpg`（剧集缩略图）。季海报可能在季目录里，
  也可能统一放在剧集根目录（`season01-poster.jpg`），两处都会找。
- 字幕：`.srt` / `.ass` / `.ssa` / `.vtt`，按 stem 配对，语言从文件名猜
  （中/英/日/韩/俄/法/西，猜不出用 `--sub-lang-default`，默认 `zho`）。
- 音轨语言：nfo 里 `<streamdetails><audio><language>` 和复数 `<languages>` 都认。
- 外链：`ProviderIds` 里的 IMDb / TMDB / TheTVDB 会拼成 `ExternalUrls`（纯字符串，不发请求）。

图片优先用目录里自带的（跟站点同域，国内直连稳）；没有才退回 nfo 里的 `image.tmdb.org`。

## 用法

六个子命令：`scan` / `build` / `push` / `run` / `status` / `reset`。

### 1）先看能扫到什么（只列目录，不解析 NFO）

```bash
python scripts/tools/xiaoya_import.py scan --category doc --limit 30
```

### 2）抓下来生成导入 JSON

```bash
python scripts/tools/xiaoya_import.py build --category movie --limit 10 --skip 115 \
    --out dist/xiaoya/movie.json
```

### 3）推给 FakEmby（自动建库、自动分批）

```bash
python scripts/tools/xiaoya_import.py push --file dist/xiaoya/movie.json \
    --server http://localhost:8096 --api-key <管理密钥>
```

也可以不带 `--file`，直接从进度库推还没推过的：

```bash
python scripts/tools/xiaoya_import.py push --category movie \
    --server http://localhost:8096 --api-key <管理密钥>
```

库类型按分类定：电影 → `movies`，电视剧/动漫/综艺 → `tvshows`。库已存在就跳过建库。

### 4）一把梭（抓完直接推）

```bash
python scripts/tools/xiaoya_import.py run --category daily --limit 10 \
    --max-seasons 3 --max-episodes 8 \
    --server http://localhost:8096 --api-key <管理密钥>
```

### 5）看进度 / 清进度

```bash
python scripts/tools/xiaoya_import.py status --category all
python scripts/tools/xiaoya_import.py reset --category doc
```

## 断点续传

进度分两处存，位置由 `--state`（默认 `dist/xiaoya/state.json`）与
`--items-dir`（默认 `dist/xiaoya/items`）决定：

- `state.json`：每个目录的**内容签名**（目录清单的 sha1）、构建状态、`pushed_sig`
- `items/<分类>.jsonl`：构建出的条目，**一行一个目录**（追加写，后写的覆盖先写的）

关键性质：

- 每处理 `--flush-every`（默认 5）个目录就存一次盘，`Ctrl+C` 或进程崩了都不丢已做完的部分
  （收到中断会打印「进度已存盘」并退出 130）。
- 重跑时同一目录签名没变就直接复用，不再抓取、不再重建。
- 推送侧单独记 `pushed_sig`：构建过但没推成功的，下次 `push` 会自动带上。
- state 的 `version` 与工具不一致时提示从头开始，不会拿旧格式硬解。

## 日常增量更新

每天跑一次就够了：

```bash
python scripts/tools/xiaoya_import.py run --category daily \
    --since-days 7 --cache-ttl 86400 \
    --server http://localhost:8096 --api-key <管理密钥>
```

判定顺序：

1. **目录签名变了** → 重建（子目录的修改时间也在签名里，所以季里加了新集会带动整部剧重建）
2. **签名没变** → 复用，一个请求都不发
3. **签名没变但没推过** → 只推，不重建
4. **已知条目且目录修改时间早于 `--since-days`** → 推迟，这次不跟
   （**新目录不受此限制**——第一次见到的目录永远会处理，不然新片就永远进不来）

`--cache-ttl` 是目录列表的保鲜秒数，日常增量建议 86400（一天刷一次目录清单）；
同一天内重跑用默认 0（不刷新）可以秒回。nfo / strm 这类内容文件默认永久缓存——
小雅不会去改一个已发布条目的 nfo 内容，变了也是整目录签名先变。

其他开关：

| 参数 | 说明 |
| --- | --- |
| `--full` | 忽略签名，全部重建 |
| `--push-all` | 忽略 `pushed_sig`，全量重推 |
| `--retry-failed` | 只重试上次失败的目录 |
| `--compact` | 构建后压缩 JSONL 去重 |
| `--offset N` | 跳过前 N 个目录（配合 `--limit` 分批） |

## 参数

| 参数 | 说明 |
| --- | --- |
| `--base-url` | 站点根，默认 `https://emby.xiaoya.pro` |
| `--category` | 逗号分隔的分类名，或 `daily` / `all` |
| `--limit` / `--offset` | 每个分类抓几个目录 / 跳过前几个，0=不限 |
| `--max-depth` / `--max-seasons` / `--max-episodes` | 控制抓取深度与剧集规模，0=不限 |
| `--skip` | 跳过的目录名，逗号分隔（例：`--skip 115`） |
| `--library` | 覆盖默认库名 |
| `--out` / `--file` | build 输出 / push 输入的 JSON 路径 |
| `--server` / `--api-key` | 推送目标，api-key 也可走 `FAKEMBY_ADMIN_API_KEY` |
| `--source-url-rewrite OLD=NEW` | 直链前缀重写，可多次 |
| `--check-sources N` | 导入前 HEAD 抽查 N 条直链 |
| `--sub-lang-default` | 字幕文件名认不出语言时的默认值，默认 `zho` |
| `--state` / `--items-dir` | 进度文件与条目仓库位置 |
| `--cache-dir` / `--no-cache` / `--cache-ttl` | 抓取缓存（默认 `dist/cache/xiaoya`） |
| `--workers` / `--timeout` / `--retries` | 并发与容错 |

## 全量导入要注意

1. **先小批量试再放开**。`--limit 10 --max-seasons 2 --max-episodes 5` 跑一轮，
   看库里对不对，再去掉限制。
2. **分批靠 `--limit` + `--offset`**，别一次全上：一个批次的请求体上限 8 MB、
   1000 个顶层条目、5000 个节点（工具已按这个切批。切批的粒度是「条目」，
   一个散装目录的几百部片可能被切到多批，推送成功按目录记进度）。
3. **剧集的抓取量很大**。一季几十集，全量是几千次请求。工具把整季的 strm 直链
   一次批量抓完再本地配对，散装目录同理；缓存开着的话重跑很快。
4. **直链能不能播不由本工具保证**。strm 里写的是小雅自己的 alist 地址
   （`xiaoya.host:5678`），导入时不会去连。想换成自己的 alist：

   ```bash
   --source-url-rewrite http://xiaoya.host:5678=http://192.168.1.5:5678
   ```

   导入前想体检一下就用 `--check-sources 5`，它会 HEAD 几条直链并打印状态码。
5. **图片**：目录自带图优先；退回 `image.tmdb.org` 时，客户端所在网络得能访问，否则海报空白。
6. **重复导入是幂等的**——同一库里按名称 / ProviderIds 命中已有条目，只会更新，不会翻倍。

## 实测（2026-09-19）

- `doc` 分类两个散装目录（`纪录片/NHK` 288 部 + `纪录片/【历史影像】` 71 部）共 359 条，
  约 30 秒完成；推送到临时实例 2 批全部 200，落库 359 条目 / 359 播放源 / 359 图片 / **237 条字幕**。
- `lib_movie` 的 `电影/人人影视电影合集` 一个目录出 678 条，加 `电影/4K系列/DC系列` 共 680 条：
  680 条带播放源，672 条带图片，679 条带 IMDb/TMDB 外链，124 条有音轨语言。
- 剧集：`每日更新/电视剧/TVB Viu/叠影狙击 (2023)` 是平铺剧集，48 个文件（24 集 × 国语/粤语）
  归成 Series / 1 季 / 24 集，多轨合成一集多条源；`每日更新/动漫` 的常规剧（带 Season 目录）
  季海报与集缩略图都在。
- 断点续传：全新 state 首次构建 359 条 → 第二次「复用 2，待处理 0」，导出的 JSON 仍是完整 359 条。
- 站点图片与 tmdb 图片均可达；`xiaoya.host:5678` 从本机连不上（直连重置、走代理 502），
  所以只验证了元数据层，播放需要换成可达的 alist 地址后再试。
