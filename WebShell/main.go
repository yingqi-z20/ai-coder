package main

import (
	"flag"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "0.0.0.0:4816", "http service address")

var Upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	err := r.SetTrustedProxies([]string{"10.128.1.81"})
	if err != nil {
		panic(err)
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	flag.Parse()
	r.GET("/vivado", Echo)
	r.POST("/chat", Chat)
	r.GET("/qwen", Qwen)
	err = r.Run(*addr)
	if err != nil {
		panic(err)
	}
}
