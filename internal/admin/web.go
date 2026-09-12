// Package admin serves the public administration shell; every data API is separately authenticated.
package admin

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed web/*
var assets embed.FS

// RegisterWeb is idempotent so main and the compatibility route registrar may both call it.
func RegisterWeb(router *gin.Engine) {
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/admin/" {
			return
		}
	}
	for _, asset := range []struct {
		path, file, contentType string
	}{
		{"/admin/", "web/index.html", "text/html; charset=utf-8"},
		{"/admin/app.js", "web/app.js", "text/javascript; charset=utf-8"},
		{"/admin/styles.css", "web/styles.css", "text/css; charset=utf-8"},
	} {
		data, err := assets.ReadFile(asset.file)
		if err != nil {
			panic(err) // Embedded assets are a build-time invariant.
		}
		router.GET(asset.path, func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("Referrer-Policy", "no-referrer")
			c.Header("X-Frame-Options", "DENY")
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
			c.Data(http.StatusOK, asset.contentType, data)
		})
	}
}
