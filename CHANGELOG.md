# 更新日志

## [Unreleased]

### 新增

- M4-3：用 GoReleaser v2 产出 Linux amd64/arm64 发布归档（含 SHA-256 校验和），并把带版本号的 Helm chart 作为 GitHub Release 附件一并发布。
- M4-3：由 tag 触发的发布工作流会构建并向 `ghcr.io/<repository-owner>/<repository-name>` 推送多平台镜像。发布前校验 Go 测试、GoReleaser 配置、Compose 与 Helm；影响发布资产的 Pull Request 与手动运行只做校验。
- M4-4：极简 Helm chart —— Service、保留的 SQLite PVC（或既有 claim）、引用既有 Secret、可选的配置 ConfigMap、启动/就绪/存活探针，以及受限的 pod/容器安全上下文。
- Emby API 兼容：新增 `GET/HEAD /emby/Videos/{id}/original`（可带容器后缀）与 `/emby/Videos/{id}/stream` 的 HEAD 支持，偏好 `original` 的客户端（多数 2025 年一代的播放器）能直接起播，不再收到 404/405。
- Emby API 兼容：`/emby/Videos/{id}/master.m3u8` 与 `/main.m3u8` 的伪 HLS 播放列表。它们声明支持 HLS 并指向既有的直链地址；不生成分片，不涉及转码。
- Emby API 兼容：Web 客户端登录页所需的端点改为匿名可访问 —— `Branding/Configuration`、`Branding/Css(.css)`、`Localization/{Cultures,Countries,Options,ParentalRatings}`、`Startup/Configuration`、`System/Ext/ServerDomains`、`/emby/web/manifest.json`、`Sessions/Capabilities`。
- Emby API 兼容：新增端点 —— `/emby/Items/{id}`（裸条目详情）、`/emby/Library/MediaFolders`、`/emby/Items/Latest`、`/emby/Items/Resume`、`/emby/Users/{uid}/Items/Counts`、`/emby/Users/{uid}/Shows/{id}/{Seasons,Episodes}`、`/emby/DisplayPreferences/{id}`、`/emby/MediaSegments/{id}`、`/emby/Playback/BitrateTest`、不带媒体源 ID 的字幕，以及 `/emby` 根存活探测。
- 认证：新增 token 传递通道 —— `X-MediaBrowser-Token`、`X-MediaBrowser-Authorization`、`Authorization: MediaBrowser Token="..."`，以及 `apiKey`、`ApiKey`、`token` 三个查询参数。
- 元数据：`MediaItem` 新增 `Countries` 与 `Languages`（以 JSON 存储，出现在 `BaseItemDto` 上，可通过导入与管理端条目 API 写入）。
- 元数据：`/emby/Persons` 现在从可见条目聚合真实人物，不再返回空桩；`/emby/Persons/{id}` 解析人物，而不是按媒体 ID 查找。
- 元数据：`?GenreIds=` 与 `?StudioIds=` 过滤器现在会把虚拟条目 ID 还原成名字，因此 `/emby/Genres` 与 `/emby/Studios` 返回的过滤值能真正匹配上。
- 工具：`scripts/tools/xiaoya_import.py` —— 把小雅（emby.xiaoya.pro）的 NFO 元数据、自带图片、strm 直链与字幕翻译成 `/api/admin/import` 请求，支持 scan / build / push / run / status / reset 六种子命令、目录抓取缓存、库类型自动设置、按上限分批、直链前缀重写与导入前直链体检。用法见 `docs/XIAOYA_IMPORT.md`。
- 工具：小雅导入覆盖面从「每日更新」3 个分类扩到站点 10 个分类 —— 新增每日更新/动漫剧场版、电影库、电视剧库、动漫库、纪录片、纪录片（已刮削）、综艺；`--category` 支持 `daily`（每日更新四项）与 `all`（全部）。
- 工具：小雅导入支持**断点续传** —— 进度分两份落盘（`state.json` 记每个目录的内容签名与构建/推送状态，`items/<分类>.jsonl` 存构建结果），每 `--flush-every`（默认 5）个目录存一次盘，中断或崩溃后重跑只补没做完的；`Ctrl+C` 会先存盘再退出。
- 工具：小雅导入支持**日常增量更新** —— 目录内容签名未变即复用（一个请求都不发），签名变了才重建；`--since-days` 只跟进最近 N 天动过的已知条目（新目录不受限），`--cache-ttl` 控制目录列表保鲜；推送侧单独记 `pushed_sig`，构建过但没推成功的下次自动补推。配套 `--full` / `--push-all` / `--retry-failed` / `--compact` 与 `status` / `reset` 两个子命令。
- 工具：小雅导入支持**集平铺在剧目录里**的剧集（`tvshow.nfo` 但没有 `Season N/` 目录，小雅的 TVB Viu 就是这么摆的）：按集号归组，同一集的多条音轨（`01粤语` + `01国语`）合成一集多条源，而不是两集同名条目。
- 工具：小雅导入新增元数据字段 —— 外链（由 ProviderIds 拼 IMDb/TMDB/TheTVDB，不发请求）、音轨语言（nfo 的 `<language>` 与复数 `<languages>` 都认）、按 stem 配对的字幕与语言猜测（`--sub-lang-default`，默认 `zho`）、跟片名走的图片（`{stem}-poster` / `-thumb` / `-fanart` 等）与季海报（季目录找不到时回退剧集根目录）。
- Emby API 兼容：新增 `GET /emby/Shows` 与 `GET /emby/Movies`（含小写变体），默认分别按 Series / Movie 过滤。官方客户端进入库视图时打的就是这两个路径，此前返回 404。
- Emby API 兼容：新增推荐位端点 `Movies/Recommendations`、`Shows/Recommendations`、`Items/{id}/Recommendations`（含带用户 ID 的变体），返回空结果集。客户端首页请求它们时收到 404 会让整行推荐消失。
- Emby API 兼容：新增 `GET /emby/ItemTypes`，返回命中搜索词的类型清单（`{Items:[{Name,Count}]}`，按命中数降序，只含 Movie/Series/Season/Episode）。官方客户端搜索页是 `Promise.all([/emby/Users/{uid}/Items, /emby/ItemTypes])`，用后者的 `Items` 渲染"电影 / 剧集 / …"分类行；缺这个端点时整条 Promise 链 reject，表现为输入关键词后什么都不显示。
- 工具：`scripts/dev/probe_similar_person.py` —— 对着已启动的服务实测"相似条目"与"人物详情"两条链路：`Similar?IncludeItemTypes=Program` 必须为空、`Similar` 不带类型必须非空、人物/分类列表与详情 DTO 必须带 `ServerId`。用法 `python scripts/dev/probe_similar_person.py`（可用 `FAKEMBY_PROBE_BASE` / `_USER` / `_PW` 覆盖）。
- 工具：`scripts/dev/probe_favorites.py` —— 实测收藏链路：`/emby/Persons`、`/emby/Genres`、`/emby/Studios` 带 `Filters=IsFavorite` 时的过滤是否正确；收藏后两种取消形式（DELETE 与 `POST .../Delete`）都要能回到基线。脚本以"当前已收藏集合"为基线做断言，不会破坏已有收藏。
- 配置：新增 `image.require_auth`（默认 `false`，可用 `FAKEMBY_IMAGE_REQUIRE_AUTH` 覆盖）。为 `true` 时图片端点强制校验凭据；默认关闭，与 Emby 官方一致。

### 变更

- 请求路径归一化改为从已注册的 gin 路由推导规范形式，不再依赖七条硬编码映射。不带 `/emby` 前缀的请求（例如 `/System/Info/Public`）与大小写变体（例如 `/emby/system/info/public`）会解析到同一批 handler。Emby 命名空间之外的路径（`/api/*`、`/admin/`、`/healthz`、`/readyz`、`/metrics`、WebSocket 别名）不受影响。
- 图片端点（`/emby/Items/{id}/Images/{type}[/{index}]`）接受有效 token **或**有效签名，不再强制要求 token；像 Infuse 这样重放缓存图片 URL 而不带 token 的客户端不会再丢海报。
- 条目图片端点同时应答 HEAD 请求。
- 管理端创建单个条目时，现在会用确定性 ID 写入人物，并创建 genre/studio/person 虚拟条目，与批量导入的行为一致。此前手工创建的条目不会出现在 `/emby/Persons`，也无法被 `?PersonIds=` 找到。
- Docker 构建改用 Go 1.26.3 与 BuildKit 目标平台参数，不再强制 amd64。运行时使用 UID/GID 10001，镜像只包含二进制与运行时依赖，不含仓库的配置或本地数据。
- Compose 改为只读根文件系统、丢弃 capabilities、no-new-privileges、有界 tmpfs、轮转容器日志、持久命名卷，默认绑定回环地址；需要局域网/反代访问时显式设置 `FAKEMBY_BIND_ADDRESS`。
- Compose 要求从 shell 或密钥管理器提供 `FAKEMBY_ADMIN_API_KEY` 与 `FAKEMBY_PLAYBACK_SIGN_KEY`，不再内置空值或占位凭据。同时删除部署示例中未使用的刮削器环境变量。
- 容器内文件日志重定向到 `/dev/null`；应用日志仍写 stderr。数据库与图片缓存仍写在 `/app/data` 下。
- 探针改为对既有的 `/emby/System/Info/Public` 路由发 GET。它们检查的是 HTTP 是否响应，不是数据库是否就绪；这些部署变更没有新增健康检查端点。

### 修复

- 小雅导入：进度仓库改成「一行一个目录」。此前一行一条目、按目录 key 覆盖，散装目录（一个目录几百部片）重跑或 `--push-all` 时只剩最后一条，导出和推送都会大面积丢条目。
- 小雅导入：散装目录的判定提到 `movie.nfo` / `tvshow.nfo` 之前。此前「目录里有一部剧 + 一堆平铺的片」会被判成单条目，只出 1 条且名字取错。
- 小雅导入：nfo 解析失败时不再整条丢弃。小雅少数 nfo 把两个根元素直接拼在一个文件里（如 `纪录片/【历史影像】`），现在包一层假根取第一个有效节点。
- 小雅导入：目录发现改为分块推进，不再每层全展开。此前扫 6 条结果要发 574 次请求，现在 95 次。
- 管理端删除用户不再无条件返回成功。`db.Transaction(...)` 返回的本身就是 `error`，多取一次 `.Error` 拿到的是该错误的 `Error` 方法值（永不为 `nil`），失败被完全吞掉，接口总是返回 `204`。
- `/emby/Branding/Configuration` 不再要求认证；Web 客户端在还没有 token 时就要从登录页取它，此前每次请求都返回 401。
- `/emby/Sessions/Capabilities`（非 `Full` 变体）现在匿名可访问，并接受 GET、POST、HEAD；客户端常在拿到有效 token 之前上报设备能力。
- `Authorization: MediaBrowser Token="..."` 与 `X-MediaBrowser-*` 头此前不被解析，整个认证串被当作 token 比对，导致有效凭据反复失败且看不出原因。
- `/emby/Persons/{id}` 此前按媒体 ID 查库，打开演员卡片必然失败；现在解析人物。路由参数同时改名，避免访问控制把它当成媒体 ID 而返回 403。
- Genre、studio、person 的 ID 现在由同一个共享函数生成（`database.VirtualItemID`）。此前三处各自计算 `md5(prefix + ":" + name)`，任何不一致都会让 `?PersonIds=` 过滤静默失效且不报错。
- 面向客户端的数组与 map 字段统一在一处初始化（`types.NewBaseItemDto`）。新增 `Countries` 与 `Languages` 时曾让三处构造点输出 `null`，而官方客户端不做空值判断。
- 路由注册改为服务端与测试脚手架共用一份，两者使用同一套路径归一化中间件。此前各自维护一份清单，新增端点在测试里返回 404。
- 季的 `IndexNumber` 现在填季号（此前只填集号，季恒为 `null`），客户端据此排序与显示季序号；季的 `ParentIndexNumber` 不再误填。
- 图片端点默认允许匿名读取。官方客户端（Emby Theater）是用 `<img src>` 拉图的：`apiclient.getImageUrl` 不拼 `api_key`，浏览器也无法给图片请求附加请求头，强制鉴权会让海报整片 401。带凭据的请求仍然完整校验，"带错凭据"不会比"不带"更宽松；需要收紧时设 `image.require_auth = true`。
- 条目图片现在按回退链找图：本条目指定类型 → 同条目 `Thumb`/`Backdrop` → 沿父链（Episode→Season→Series）逐级找 `Primary`/`Thumb`/`Backdrop`。官方客户端只请求 `Primary`，而集和季常常只有 `Thumb` 或没有图，此前这些卡片一律 404 显示空白。
- 访问控制不再拒绝 Genre / Studio / Person 虚拟条目。它们没有 `library_id`，被"必须属于某个库"的谓词判为越权：客户端点演员头像时请求 `/emby/Users/{uid}/Items/{人物ID}` 得到 403，详情页的 `Promise.all` 整体 reject，显示 "Content no longer available"。按 ID 直接访问时放行，列表查询仍受作用域约束；同时新增 `virtualItemDTO`，让条目详情与 `Similar` 都能正确应答这类 ID。
- 媒体库的 `Subviews` 按类型生成（`tvshows` 含 `series` / `episodes` / `studios`，`movies` 含 `movies` / `videos` 等），不再一律返回 `[库类型, tags, genres, folders]`。Emby Theater 的 `tv/tv.js` 按 `subviews.includes("series")` 决定"剧集"入口是否显示，按 `includes("episodes")` 决定"单集"视图是否显示——清单里没有这两项时，剧集库的分类栏会少掉整个"剧集"分类，也进不了单集视图（连带只在单集视图里出现的"节目名称"排序也用不上）。
- `PlaybackInfo` 对没有播放源的条目（例如整部剧）返回空 `MediaSources`，不再返回 404。官方客户端进详情页时会顺带拉一次用于背景预览，404 会打断请求链。
- 路径归一化的路由缓存不再把"未命中"存成 typed-nil。`(*routePattern)(nil)` 存进 `sync.Map` 后，下次读取时类型断言会成功但值是 `nil`，紧接着的解引用就是 nil 指针 panic —— 表现为**同一个未注册路径的第二次请求连接被掐断**，客户端拿到网络错误而不是 404。官方客户端搜索页的 `Promise.all` 里只要有一条被打断，整个搜索就一片空白。
- 访问被拒时记录审计日志（用户、条目 ID、路径、来源 IP），此前这类 403 完全无声，只能靠猜。
- `/emby/Items/{id}/Similar` 现在遵循 `?IncludeItemTypes=`。Emby Theater 的 `itemhelper.supportsSimilarItemsOnLiveTV` 对 Movie / Trailer / Series **无条件返回 true**，是否显示"更多类似的直播电视"栏目完全取决于该请求是否返回空。此前忽略过滤参数，于是拿库里的电影去填 `IncludeItemTypes=Program` 的请求，详情页凭空多出一栏直播电视。没有直播电视时现在答空集，客户端自行隐藏整栏。
- 虚拟条目与分类列表的 DTO 现在回填 `ServerId`（人物详情、`/emby/Genres`、`/emby/Studios`、`/emby/Persons`）。客户端用 `item.ServerId` 反查 apiClient，缺失时 `connectionManager.getApiClient(item)` 返回 undefined，紧接着的 `apiClient.getItems(...)` 抛 TypeError，人物详情页整页空白。
- 相似条目不再因为条件过严而返回空集。`FindSimilarItems` 原先同时要求"同类型 + 同流派 + 年份 ±3"，三者一起收紧时命中率极低（实测小样本库里电影的相似项恒为 0 条，"类似影片"整栏消失）。改为逐级放宽——同类型+同流派+相近年份 → +同流派 → +相近年份 → 仅同类型——并逐级去重累积到 `Limit` 为止，相关度高的排在前面。
- 列表端点现在认 `?Filters=IsFavorite`（`/emby/Persons`、`/emby/Genres`、`/emby/Studios`）。官方客户端「喜欢」页的人物栏打的是 `apiClient.getPeople`（即 `/emby/Persons`），而不是 `/emby/Items`，并带上 `Filters=IsFavorite`；此前完全忽略这个参数，于是库里每个人物都被当成"已收藏"——没收藏过任何人也会长出一整栏"喜欢的人物"（小雅测试库 312 条）。收藏状态仍以 `play_progress.is_favorite` 为准，收藏某个人物后会正常出现在该栏。
- Emby API 兼容：新增 `POST /emby/Users/{uid}/FavoriteItems/{id}/Delete` 与 `POST /emby/Users/{uid}/PlayedItems/{id}/Delete`。官方客户端 `apiclient.js` 里有 `this._enablePostForDelete = this.isMinServerVersion("4.7.0.33")`，我们对外声明 4.8.0.0，因此"取消收藏 / 取消已看"走的是这个 POST + `/Delete` 形式，而不是 DELETE。此前只注册了 DELETE，客户端取消收藏必然收到 404 —— 表现为"能加喜欢，不能取消喜欢"。两种形式现在都可用。

### 部署与升级须知

- 更换存储前先备份现有 SQLite 部署。干净停掉旧实例，再把 `./data/db` 的内容（含一致的 WAL 状态）迁进新命名卷，归属改为 `10001:10001`。不要在没有一致 WAL 状态的情况下复制运行中的 SQLite 数据库。或者保留旧的 bind mount 到 `/app/data` 并匹配归属。没有自动迁移，空命名卷会新建数据库。旧缓存可选；文件日志不再挂载。
- 在 `docker compose up --build -d` 之前，为两个必需的 Compose 变量导出强、互不相同且持久的值。环境变量对 Docker 管理员仍然可见。不要提交含密钥的 `.env`，也不要分享渲染后带凭据的 Compose 配置。本应用**不**实现 `secret_file` 或 `*_FILE` 配置；仅挂载一个密钥文件不会生效。
- 配置文件是可选的。按需取消 Compose 只读挂载的注释，或在 Helm 中提供 `config.existingConfigMap`。只使用受支持的 `FAKEMBY_*` 环境变量，部署环境变量的值会覆盖挂载的配置文件。镜像内不含仓库本地的 `config.yaml`。
- 安装 Helm 前，在发布所在命名空间按 `secrets.existingSecret` 指定的名称（默认 `fakemby-secrets`）创建 Secret，键 `admin-api-key` 与 `playback-sign-key` 必须非空。必要时在 values 中覆盖键名。values/history 不需要包含密钥值。轮换密钥或改动可选的 subPath 挂载 ConfigMap 后，重启 Deployment。
- chart 固定单副本并使用 `Recreate`；升级可能短暂中断服务。不要扩容 Deployment，也不要与其他 release 共用 SQLite 卷。存储驱动需支持 `fsGroup`，才能把 claim 交给 UID/GID 10001 可写；驱动做不到就另行设置归属。chart 创建的 PVC 在卸载时保留；重装时用 `persistence.existingClaim` 接管保留的数据。
- 源码 chart 的 `appVersion` 记录的是历史基线，不代表该 tag 存在对应镜像。从源码安装时须用已发布版本覆盖 `image.tag`。发布的 chart 包会自动带上实际发布版本。fork 必须把 `image.repository` 覆盖成自己的仓库路径。

### 发布约定

- 发布 tag 使用 `vMAJOR.MINOR.PATCH` 或 `vMAJOR.MINOR.PATCH-PRERELEASE`，数字标识符与构建元数据不带前导零。例：`v0.10.0`、`v0.10.0-rc.1`。这些约定同时兼容 SemVer 与容器 tag。
- 打 tag 前，把相关条目从 Unreleased 移到带日期的版本小节。应用发布版本与 `server.version` 分开维护，后者声明的是 Emby 协议兼容版本。
- 推送 tag 会发布 GitHub Release 的归档/校验和/chart 与 amd64/arm64 镜像。容器版本 tag 不带前导 `v`；只有稳定版会更新 `latest`。预发布 tag 创建 GitHub 预发布，不更新 `latest`。
- 工作流需要写入仓库 release 与 GHCR 包的 GitHub Actions 权限。它从仓库派生 registry 归属，不硬编码到上游账号。
- 本地校验可用 `goreleaser check`、`helm lint --strict deploy/helm/fakemby`、`helm template fakemby deploy/helm/fakemby`、`docker compose config --quiet`（配非密钥的校验值）。GoReleaser 输出到 `dist/release`，不是既有的 `dist` 运行时目录；工作流把 chart 单独打包到 `dist/charts`。

## [0.9.0-pre]

- 路线图引用的历史预发布基线 tag。本条目不代表该 tag 曾发布归档或容器镜像。
