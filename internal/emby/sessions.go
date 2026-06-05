package emby

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PlayingRequest struct {
	ItemId              string `json:"ItemId" binding:"required"`
	MediaSourceId       string `json:"MediaSourceId"`
	PlaySessionId       string `json:"PlaySessionId"`
	PositionTicks       int64  `json:"PositionTicks"`
	IsPaused            bool   `json:"IsPaused"`
	IsMuted             bool   `json:"IsMuted"`
	PlaybackRate        float64 `json:"PlaybackRate"`
	VolumeLevel         int    `json:"VolumeLevel"`
	Brightness          int    `json:"Brightness"`
	AspectRatio         string `json:"AspectRatio"`
	PlayMethod          string `json:"PlayMethod"`
}

type StoppedRequest struct {
	ItemId        string `json:"ItemId" binding:"required"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks int64  `json:"PositionTicks"`
}

type ProgressRequest struct {
	ItemId        string `json:"ItemId" binding:"required"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks int64  `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
}

// ProgressBuffer 播放进度缓冲器（去抖缓冲，30s 批量写入）
type ProgressBuffer struct {
	mu      sync.RWMutex
	buffer  map[string]bufferedProgress // key: user_id:item_id
	ticker  *time.Ticker
	done    chan struct{}
	playSvc *service.PlaybackService
}

type bufferedProgress struct {
	userID        string
	itemID        string
	positionTicks int64
	lastPlayed    time.Time
	isPlayed      bool
	playCount     int
	isFavorite    bool
}

var progressBuffer *ProgressBuffer

func InitProgressBuffer(db *gorm.DB, flushInterval time.Duration) {
	progressBuffer = &ProgressBuffer{
		buffer:  make(map[string]bufferedProgress),
		ticker:  time.NewTicker(flushInterval),
		done:    make(chan struct{}),
		playSvc: service.NewPlaybackService(db),
	}

	go progressBuffer.flushLoop()
	slog.Info("进度缓冲系统启动", "flush_interval", flushInterval)
}

// flushLoop 后台 goroutine，定期 flush 缓冲数据
func (pb *ProgressBuffer) flushLoop() {
	for {
		select {
		case <-pb.ticker.C:
			pb.flush()
		case <-pb.done:
			// 关闭前 flush 所有剩余数据
			pb.flush()
			return
		}
	}
}

// flush 将缓冲数据写入数据库（单个事务）
func (pb *ProgressBuffer) flush() {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if len(pb.buffer) == 0 {
		return
	}

	// 批量更新数据库
	tx := database.Get().Begin()

	for _, prog := range pb.buffer {
		var progress database.PlayProgress
		if err := tx.Where("user_id = ? AND item_id = ?", prog.userID, prog.itemID).First(&progress).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// 创建新记录
				progress = database.PlayProgress{
					UserID:        prog.userID,
					ItemID:        prog.itemID,
					PositionTicks: prog.positionTicks,
					LastPlayed:    &prog.lastPlayed,
				}
				tx.Create(&progress)
			}
		} else {
			// 更新现有记录
			tx.Model(&progress).Updates(map[string]interface{}{
				"position_ticks": prog.positionTicks,
				"last_played":    prog.lastPlayed,
				"is_played":      prog.isPlayed,
				"play_count":     prog.playCount,
			})
		}
	}

	if err := tx.Commit().Error; err != nil {
		slog.Error("进度缓冲 flush 失败", "error", err)
	} else {
		slog.Debug("进度缓冲 flush 完成", "count", len(pb.buffer))
	}

	// 清空缓冲
	pb.buffer = make(map[string]bufferedProgress)
}

// ShutdownProgressBuffer 关闭缓冲系统
func ShutdownProgressBuffer() {
	if progressBuffer != nil {
		progressBuffer.ticker.Stop()
		close(progressBuffer.done)
	}
}

func RegisterSessionRoutes(router *gin.Engine) {
	router.POST("/emby/Sessions/Playing", AuthTokenMiddleware(30), playingStart())
	router.POST("/emby/Sessions/Playing/Progress", AuthTokenMiddleware(30), playingProgress())
	router.POST("/emby/Sessions/Playing/Stopped", AuthTokenMiddleware(30), playingStopped())
}

func playingStart() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")

		var req PlayingRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 记录播放开始
		slog.Info("播放开始",
			"user_id", userID,
			"item_id", req.ItemId,
			"position_ticks", req.PositionTicks,
		)

		// 添加到缓冲（更新 last_played）
		if progressBuffer != nil {
			progressBuffer.mu.Lock()
			key := userID + ":" + req.ItemId
			prog := progressBuffer.buffer[key]
			prog.userID = userID
			prog.itemID = req.ItemId
			prog.positionTicks = req.PositionTicks
			prog.lastPlayed = time.Now()
			progressBuffer.buffer[key] = prog
			progressBuffer.mu.Unlock()
		}

		c.Status(http.StatusNoContent)
	}
}

func playingProgress() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")

		var req ProgressRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 添加到缓冲（仅更新进度，不立即写入 DB）
		if progressBuffer != nil {
			progressBuffer.mu.Lock()
			key := userID + ":" + req.ItemId
			prog := progressBuffer.buffer[key]
			prog.userID = userID
			prog.itemID = req.ItemId
			prog.positionTicks = req.PositionTicks
			if prog.lastPlayed.IsZero() {
				prog.lastPlayed = time.Now()
			}
			progressBuffer.buffer[key] = prog
			progressBuffer.mu.Unlock()

			slog.Debug("进度更新（缓冲）",
				"user_id", userID,
				"item_id", req.ItemId,
				"position_ticks", req.PositionTicks,
			)
		}

		c.Status(http.StatusNoContent)
	}
}

func playingStopped() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")

		var req StoppedRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 停止播放时立即 flush 该项的进度到 DB
		if progressBuffer != nil {
			progressBuffer.mu.Lock()
			key := userID + ":" + req.ItemId

			prog := progressBuffer.buffer[key]
			prog.userID = userID
			prog.itemID = req.ItemId
			prog.positionTicks = req.PositionTicks
			prog.lastPlayed = time.Now()

			// 判断是否标记为已看（进度 > 90%）
			item, err := service.NewMediaService(database.Get()).GetItemByID(req.ItemId)
			if err == nil && item.RuntimeTicks != nil && *item.RuntimeTicks > 0 {
				if req.PositionTicks > (*item.RuntimeTicks * 9 / 10) {
					prog.isPlayed = true
					prog.playCount++
				}
			}

			progressBuffer.buffer[key] = prog

			// 立即 flush 这一项
			var progress database.PlayProgress
			if err := database.Get().Where("user_id = ? AND item_id = ?", userID, req.ItemId).First(&progress).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					progress = database.PlayProgress{
						UserID:        userID,
						ItemID:        req.ItemId,
						PositionTicks: prog.positionTicks,
						IsPlayed:      prog.isPlayed,
						PlayCount:     prog.playCount,
						LastPlayed:    &prog.lastPlayed,
					}
					database.Get().Create(&progress)
				}
			} else {
				database.Get().Model(&progress).Updates(prog)
			}

			// 从缓冲中删除
			delete(progressBuffer.buffer, key)
			progressBuffer.mu.Unlock()

			slog.Info("播放停止（进度已保存）",
				"user_id", userID,
				"item_id", req.ItemId,
				"position_ticks", req.PositionTicks,
				"is_played", prog.isPlayed,
			)
		}

		c.Status(http.StatusNoContent)
	}
}
