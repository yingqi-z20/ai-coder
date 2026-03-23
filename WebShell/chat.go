package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

var requests = make(chan string, 16)

func Chat(c *gin.Context) {
	m := make(map[string]string)
	err := c.ShouldBindJSON(&m)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		c.Abort()
		return
	}
	requests <- m["message"]
	c.JSON(http.StatusOK, gin.H{})
}

func Qwen(c *gin.Context) {
	conn, err := Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Info("upgrade error:", err)
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
					return
				}
			}
		}
	}()
	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {
			slog.Info("close error:", err)
		}
	}(conn)
	client := openai.NewClient(option.WithAPIKey("sk-dhm7BpRELwFatKM9G67x9w"), option.WithBaseURL("10.128.8.21:4000"))
	ctx := context.Background()
	tools := []map[string]string{
		{
			"type": "web_search",
		},
		{
			"type": "web_extractor",
		},
		{
			"type": "code_interpreter",
		},
	}
	response, err := client.Responses.New(ctx, responses.ResponseNewParams{
		Model: "qwen3-max",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(buildSystemPrompt()),
		},
		Store: openai.Bool(true),
	}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))

	for {
		if err != nil {
			slog.Info("new response error:", err)
			return
		}
		err = conn.WriteMessage(websocket.TextMessage, []byte(response.OutputText()))
		if err != nil {
			slog.Info("write error:", err)
			return
		}
		response, err = client.Responses.New(ctx, responses.ResponseNewParams{
			Model:              "qwen3-max",
			PreviousResponseID: openai.String(response.ID),
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String(<-requests),
			},
			Store: openai.Bool(true),
		}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))
	}
}

func buildSystemPrompt() string {
	workspaceDir := strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	if workspaceDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workspaceDir = wd
		}
	}
	if workspaceDir == "" {
		workspaceDir = "<UNKNOWN_WORKSPACE>"
	}
	return strings.ReplaceAll(Prompt, "{{WORKSPACE_DIR}}", workspaceDir)
}
