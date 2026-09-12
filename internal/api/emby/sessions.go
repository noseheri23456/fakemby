package emby

import (
	"github.com/fakemby/fakemby/internal/access"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

var activeSessionsMu sync.RWMutex
var activeSessions = make(map[string]*SessionInfo)

type NowPlayingItem struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
	Type string `json:"Type"`
}
type PlayingRequest struct {
	ItemId        string  `json:"ItemId" binding:"required"`
	MediaSourceId string  `json:"MediaSourceId"`
	PlaySessionId string  `json:"PlaySessionId"`
	PositionTicks int64   `json:"PositionTicks"`
	IsPaused      bool    `json:"IsPaused"`
	IsMuted       bool    `json:"IsMuted"`
	PlaybackRate  float64 `json:"PlaybackRate"`
	VolumeLevel   int     `json:"VolumeLevel"`
	Brightness    int     `json:"Brightness"`
	AspectRatio   string  `json:"AspectRatio"`
	PlayMethod    string  `json:"PlayMethod"`
}
type StoppedRequest = PlayingRequest
type ProgressRequest = PlayingRequest
type bufferedProgress struct {
	userID, itemID string
	positionTicks  int64
	lastPlayed     time.Time
}
type ProgressBuffer struct {
	mu             sync.Mutex
	buffer         map[string]bufferedProgress
	db             *gorm.DB
	ticker         *time.Ticker
	done, finished chan struct{}
	once           sync.Once
}

var progressBuffer *ProgressBuffer

func InitProgressBuffer(db *gorm.DB, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if db == database.Get() {
		db = database.GetWrite()
	}
	pb := &ProgressBuffer{buffer: map[string]bufferedProgress{}, db: db, ticker: time.NewTicker(interval), done: make(chan struct{}), finished: make(chan struct{})}
	progressBuffer = pb
	go func() {
		defer close(pb.finished)
		for {
			select {
			case <-pb.ticker.C:
				if err := pb.flush(); err != nil {
					slog.Error("Progress flush failed", "error", err)
				}
			case <-pb.done:
				return
			}
		}
	}()
}
func writeProgress(tx *gorm.DB, p bufferedProgress) error {
	row := database.PlayProgress{UserID: p.userID, ItemID: p.itemID, PositionTicks: p.positionTicks, LastPlayed: &p.lastPlayed}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "item_id"}}, DoUpdates: clause.AssignmentColumns([]string{"position_ticks", "last_played"})}).Create(&row).Error
}
func (pb *ProgressBuffer) flush() error {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if len(pb.buffer) == 0 {
		return nil
	}
	if err := pb.db.Transaction(func(tx *gorm.DB) error {
		for _, p := range pb.buffer {
			if err := writeProgress(tx, p); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	clear(pb.buffer)
	return nil
}
func FlushProgressNow() {
	if err := FlushProgress(); err != nil {
		slog.Error("Progress flush failed", "error", err)
	}
}
func FlushProgress() error {
	if progressBuffer == nil {
		return nil
	}
	return progressBuffer.flush()
}
func BufferProgress(user, item string, pos int64, touch bool) {
	pb := progressBuffer
	if pb == nil {
		return
	}
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.buffer[user+":"+item] = bufferedProgress{user, item, pos, time.Now().UTC()}
}
func ShutdownProgressBuffer() {
	pb := progressBuffer
	if pb == nil {
		return
	}
	pb.once.Do(func() {
		pb.ticker.Stop()
		close(pb.done)
		<-pb.finished
		for i := 0; i < 3; i++ {
			if pb.flush() == nil {
				return
			}
		}
		slog.Error("Unpersisted progress remains after shutdown retries")
	})
}
func RegisterSessionRoutes(r *gin.Engine, cfg *config.Config) {
	auth := AuthTokenMiddleware(cfg.TokenExpiryDays())
	r.POST("/emby/Sessions/Playing", auth, playingStart())
	r.POST("/emby/Sessions/Playing/Progress", auth, playingProgress())
	r.POST("/emby/Sessions/Playing/Stopped", auth, playingStopped())
	r.GET("/emby/Sessions", auth, RequireAdmin(), getSessions())
	r.POST("/emby/Sessions/:sessionId/Playing/Stop", auth, RequireAdmin(), stopSession())
	r.POST("/emby/Sessions/:sessionId/Message", auth, RequireAdmin(), sessionMessage())
	r.GET("/emby/Devices/Info", auth, getDeviceInfo())
	r.POST("/emby/Sessions/Capabilities/Full", auth, func(c *gin.Context) { c.Status(http.StatusNoContent) })
}
func getSessions() gin.HandlerFunc {
	return func(c *gin.Context) {
		activeSessionsMu.RLock()
		out := []SessionInfo{}
		for _, s := range activeSessions {
			out = append(out, *s)
		}
		activeSessionsMu.RUnlock()
		c.JSON(200, out)
	}
}
// userSessions 返回某用户的活动会话快照。
// 恒返回非 nil 切片——websocket 与 HTTP 两条链路共用，客户端会直接 `.filter`/`.length`。
func userSessions(userID string) []SessionInfo {
	activeSessionsMu.RLock()
	defer activeSessionsMu.RUnlock()
	return sessionsOfLocked(userID)
}

// sessionsOfLocked 在调用方已持有 activeSessionsMu（读或写）时使用。
// 播放上报全程持有写锁，此时绝不能再调 userSessions，否则自死锁。
func sessionsOfLocked(userID string) []SessionInfo {
	out := []SessionInfo{}
	for _, s := range activeSessions {
		if s.UserId == userID {
			out = append(out, *s)
		}
	}
	return out
}
func stopSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		activeSessionsMu.Lock()
		s := activeSessions[c.Param("sessionId")]
		delete(activeSessions, c.Param("sessionId"))
		activeSessionsMu.Unlock()
		if s != nil {
			eventHub.Broadcast(s.UserId, "Playstate", gin.H{"Command": "Stop"})
		}
		c.Status(204)
	}
}
func sessionMessage() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body map[string]any
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, ErrBadRequest)
			return
		}
		activeSessionsMu.RLock()
		s := activeSessions[c.Param("sessionId")]
		activeSessionsMu.RUnlock()
		if s == nil {
			c.JSON(404, ErrNotFound)
			return
		}
		eventHub.Broadcast(s.UserId, "DisplayMessage", body)
		c.Status(204)
	}
}
func getDeviceInfo() gin.HandlerFunc {
	return func(c *gin.Context) {
		var token database.Token
		q := database.Get().Where("device_id = ?", c.Query("Id"))
		if !c.GetBool("is_admin") {
			q = q.Where("user_id = ?", c.GetString("user_id"))
		}
		if q.Order("created_at desc").First(&token).Error != nil {
			c.JSON(404, ErrNotFound)
			return
		}
		c.JSON(200, gin.H{"Id": token.DeviceID, "Name": token.DeviceName, "LastUserId": token.UserID, "AppName": token.Client, "AppVersion": token.Version})
	}
}
func playingStart() gin.HandlerFunc    { return recordPlayback(false) }
func playingProgress() gin.HandlerFunc { return recordPlayback(false) }
func playingStopped() gin.HandlerFunc  { return recordPlayback(true) }
func recordPlayback(stopped bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req PlayingRequest
		if c.ShouldBindJSON(&req) != nil || req.PositionTicks < 0 {
			c.JSON(400, ErrBadRequest)
			return
		}
		userID := c.GetString("user_id")
		var user database.User
		var item database.MediaItem
		if database.Get().Where("id = ?", userID).First(&user).Error != nil || !access.IsPlaybackAllowed(&user) {
			c.JSON(403, ErrForbidden)
			return
		}
		if database.Get().Where("id = ?", req.ItemId).First(&item).Error != nil {
			c.JSON(404, ErrNotFound)
			return
		}
		if !access.CanAccessItem(database.Get(), &user, req.ItemId) {
			c.JSON(403, ErrForbidden)
			return
		}
		sid := req.PlaySessionId
		if sid == "" {
			sid = c.GetString("token")
		}
		if sid == "" {
			sid = userID
		}
		activeSessionsMu.Lock()
		defer activeSessionsMu.Unlock()
		now := time.Now().UTC()
		for id, s := range activeSessions {
			last, _ := time.Parse(time.RFC3339, s.LastActivityDate)
			if now.Sub(last) > 2*time.Minute {
				delete(activeSessions, id)
			}
		}
		if s, ok := activeSessions[sid]; ok && s.UserId != userID {
			c.JSON(403, ErrForbidden)
			return
		}
		if !stopped {
			n := 0
			for id, s := range activeSessions {
				if s.UserId == userID && id != sid {
					n++
				}
			}
			if max := access.SessionLimit(&user); max > 0 && n >= max {
				c.JSON(429, gin.H{"StatusCode": 429, "Message": "Concurrent stream limit reached"})
				return
			}
		}
		// Acknowledge only after durable persistence; abrupt termination cannot lose
		// acknowledged HTTP progress. The optional buffer remains for embedded callers.
		err := database.GetWrite().Transaction(func(tx *gorm.DB) error {
			if err := writeProgress(tx, bufferedProgress{userID, req.ItemId, req.PositionTicks, now}); err != nil {
				return err
			}
			if stopped && item.RuntimeTicks != nil && *item.RuntimeTicks > 0 && req.PositionTicks >= *item.RuntimeTicks*9/10 {
				if err := tx.Model(&database.PlayProgress{}).Where("user_id = ? AND item_id = ? AND is_played = ?", userID, item.ID, false).Updates(map[string]any{"is_played": true, "play_count": gorm.Expr("play_count + 1")}).Error; err != nil {
					return err
				}
			}
			if stopped {
				return tx.Create(&database.PlaybackActivity{UserID: userID, ItemID: item.ID, ItemType: item.Type, ItemName: item.Name, PlayDuration: int(req.PositionTicks / 10000000), RemoteAddress: c.ClientIP()}).Error
			}
			return nil
		})
		if err != nil {
			c.JSON(500, ErrInternal)
			return
		}
		if stopped {
			delete(activeSessions, sid)
		} else {
			// 数组字段一律初始化为空切片：客户端会裸调 `.includes`/`.length`（见 ROADMAP §10）。
			activeSessions[sid] = &SessionInfo{
				Id: sid, UserId: userID, UserName: user.Name,
				LastActivityDate: now.Format(time.RFC3339), RemoteEndPoint: c.ClientIP(),
				AdditionalUsers:    []UserDTO{},
				PlayableMediaTypes: []string{},
				SupportedCommands:  []string{},
				Capabilities: Capabilities{
					PlayableMediaTypes: []string{},
					SupportedCommands:  []string{},
				},
				NowPlayingItem: &NowPlayingItem{item.ID, item.Name, item.Type},
				PlayState:      PlayState{PositionTicks: &req.PositionTicks, IsPaused: req.IsPaused},
			}
		}
		eventHub.Broadcast(userID, "UserDataChanged", gin.H{"UserId": userID, "UserDataList": []gin.H{{"ItemId": item.ID, "PlaybackPositionTicks": req.PositionTicks}}})
		// 会话快照必须在写锁内取好：此处调用 userSessions 会重复加读锁而自死锁。
		eventHub.Broadcast(userID, "Sessions", sessionsOfLocked(userID))
		c.Status(204)
	}
}
