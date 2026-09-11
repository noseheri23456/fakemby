package emby

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
)

type CustomQueryRequest struct {
	CustomQueryString string `json:"CustomQueryString"`
	ReplaceUserId     bool   `json:"ReplaceUserId"`
}

func RegisterStatsRoutes(router *gin.Engine, cfg *config.Config) {
	// Sakura_embyboss (or rather the User Usage Stats Plugin) expects this route
	router.POST("/emby/user_usage_stats/submit_custom_query", AuthTokenMiddleware(cfg.Auth.TokenExpiryDays), RequireAdmin(), submitCustomQuery())
}

func submitCustomQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CustomQueryRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// Safety check: only allow SELECT statements
		normalized := strings.TrimSpace(strings.ToUpper(req.CustomQueryString))
		if !strings.HasPrefix(normalized, "SELECT") {
			slog.Warn("Rejected non-SELECT custom query", "query", req.CustomQueryString)
			c.JSON(http.StatusForbidden, gin.H{
				"colums":  []string{},
				"results": [][]interface{}{},
				"message": "Only SELECT queries are allowed",
			})
			return
		}

		slog.Debug("Executing custom query", "query", req.CustomQueryString)

		rows, err := database.Get().Raw(req.CustomQueryString).Rows()
		if err != nil {
			slog.Error("Failed to execute custom query", "error", err, "query", req.CustomQueryString)
			c.JSON(http.StatusOK, gin.H{
				"colums":  []string{},
				"results": [][]interface{}{},
				"message": err.Error(),
			})
			return
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		var results [][]interface{}
		for rows.Next() {
			// Create a slice of interface{}'s to represent each column,
			// and a second slice to contain pointers to each item in the columns slice.
			colValues := make([]interface{}, len(columns))
			colPtrs := make([]interface{}, len(columns))
			for i := range colValues {
				colPtrs[i] = &colValues[i]
			}

			// Scan the result into the column pointers...
			if err := rows.Scan(colPtrs...); err != nil {
				continue
			}

			// Dereference to get values
			var row []interface{}
			for i := range colValues {
				val := colValues[i]

				// Convert []byte to string for JSON serialization if necessary
				b, ok := val.([]byte)
				if ok {
					row = append(row, string(b))
				} else {
					row = append(row, val)
				}
			}
			results = append(results, row)
		}

		c.JSON(http.StatusOK, gin.H{
			"colums":  columns,
			"results": results,
		})
	}
}
