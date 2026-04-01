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

var PWD = "/tmp"

func Chat(c *gin.Context) {
	m := make(map[string]string)
	err := c.ShouldBindJSON(&m)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		c.Abort()
		return
	}
	message := m["message"]
	if len(message) > 16 && message[0:16] == "ZU1svmzfSE7zOyk " {
		PWD = message[16:]
		return
	}
	requests <- message
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
	client := openai.NewClient(option.WithAPIKey("sk-dhm7BpRELwFatKM9G67x9w"), option.WithBaseURL("http://10.128.8.21:4000"))
	ctx := context.Background()
	/*
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
	*/
	stream := client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: "qwen3.5-plus",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(buildSystemPrompt(PWD)),
		},
		Store: openai.Bool(true),
	}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true))

	for {
		if err != nil {
			slog.Info("new stream error:", err)
			return
		}
		err = conn.WriteMessage(websocket.TextMessage, []byte("<ZU1svmzfSE7zOyk>"))
		if err != nil {
			slog.Info("write error:", err)
			return
		}
		for stream.Next() {
			event := stream.Current()
			switch event.Type {
			case "response.reasoning_content.delta":
				err = conn.WriteMessage(websocket.TextMessage, []byte(" "))
				if err != nil {
					slog.Info("write error:", err)
					return
				}
				continue
			case "response.reasoning_summary_text.delta":
				err = conn.WriteMessage(websocket.TextMessage, []byte(" "))
				if err != nil {
					slog.Info("write error:", err)
					return
				}
				continue
			case "response.output_text.delta":
				err = conn.WriteMessage(websocket.TextMessage, []byte(event.Delta))
				if err != nil {
					slog.Info("write error:", err)
					return
				}
			case "response.completed":
			}
			err = conn.WriteMessage(websocket.TextMessage, []byte(event.Delta))
			if err != nil {
				slog.Info("write error:", err)
				return
			}
		}
		if err := stream.Err(); err != nil {
			slog.Info("stream error:", err)
			return
		}
		err = conn.WriteMessage(websocket.TextMessage, []byte("</ZU1svmzfSE7zOyk>"))
		if err != nil {
			slog.Info("write error:", err)
			return
		}
		prid := stream.Current().Response.ID
		stream = client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
			Model:              "qwen3.5-plus",
			PreviousResponseID: openai.String(prid),
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String(<-requests),
			},
			Store: openai.Bool(true),
		}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true))
	}
}

func buildSystemPrompt(pwd string) string {
	workspaceDir := pwd
	if workspaceDir == "" {
		workspaceDir = strings.TrimSpace(os.Getenv("WORKSPACE_DIR"))
	}
	if workspaceDir == "" {
		workspaceDir = "!!UNKNOWN_WORKSPACE!!"
	}
	return strings.ReplaceAll(Prompt, "{{WORKSPACE_DIR}}", workspaceDir)
}
