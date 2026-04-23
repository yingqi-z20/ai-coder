package main

import (
	"crypto/subtle"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
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

// AuthMiddleware API Key 认证中间件
func AuthMiddleware(expectedKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 放行 CORS 预检请求
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		// 优先从自定义请求头获取
		key := c.GetHeader("X-API-Key")
		// 兼容标准 Authorization: Bearer <key> 格式
		if key == "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				key = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}
		// 兼容 URL 查询参数 (方便浏览器/简单脚本调试)
		if key == "" {
			key = c.Query("api_key")
		}

		// 验证 Key
		if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized: invalid or missing API key",
			})
			return
		}

		c.Next()
	}
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	gin.SetMode(gin.ReleaseMode)

	// 读取环境变量 API_KEY，未设置则直接退出
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		slog.Error("API_KEY environment variable is not set")
		os.Exit(1)
	}

	r := gin.Default()
	err := r.SetTrustedProxies([]string{"10.128.1.81"})
	if err != nil {
		panic(err)
	}

	// CORS 中间件（必须放在最前面）
	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		// 添加 X-API-Key 到允许的请求头中，否则浏览器跨域时会拦截
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// 挂载全局认证中间件
	r.Use(AuthMiddleware(apiKey))

	flag.Parse()
	r.GET("/vivado", Vivado)
	r.POST("/chat", Chat)
	r.GET("/qwen", Qwen)

	err = r.Run(*addr)
	if err != nil {
		panic(err)
	}
}
