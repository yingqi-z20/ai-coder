package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

var (
	requests       = make(chan string, 16)
	pwdMu          sync.RWMutex
	PWD            = "/tmp"
	prid           atomic.Pointer[string]
	qwenConnected  atomic.Bool
	qwenGeneration atomic.Uint64
)

func getPWD() string {
	pwdMu.RLock()
	defer pwdMu.RUnlock()
	return PWD
}

func setPWD(pwd string) {
	pwdMu.Lock()
	defer pwdMu.Unlock()
	PWD = pwd
}

func previousResponseID() string {
	if id := prid.Load(); id != nil {
		return *id
	}
	return ""
}

func storePreviousResponseID(id string) {
	prid.Store(&id)
}

func drainRequests() {
	for {
		select {
		case <-requests:
		default:
			return
		}
	}
}

func Chat(c *gin.Context) {
	m := make(map[string]string)
	err := c.ShouldBindJSON(&m)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		c.Abort()
		return
	}
	slog.Debug("request", "json", m)
	message := m["message"]
	if strings.HasPrefix(message, "ZU1svmzfSE7zOyk ") {
		p := strings.TrimSpace(message[16:])
		if p != "" && p[len(p)-1] == '/' {
			p = p[:len(p)-1]
		}
		if p != "" {
			setPWD(p)
		}
		c.String(http.StatusOK, previousResponseID())
		return
	}
	if strings.HasPrefix(message, "prid") {
		message := strings.TrimSpace(message[4:])
		storePreviousResponseID(message)
		c.String(http.StatusOK, previousResponseID())
		return
	}
	if !qwenConnected.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI WebSocket is not connected"})
		return
	}
	select {
	case requests <- message:
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "chat queue is full"})
		return
	}
	c.String(http.StatusOK, previousResponseID())
}

func Qwen(c *gin.Context) {
	conn, err := Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Info("upgrade error", "err", err)
		return
	}
	generation := qwenGeneration.Add(1)
	drainRequests()
	qwenConnected.Store(true)
	defer func() {
		if qwenGeneration.Load() == generation {
			qwenConnected.Store(false)
		}
	}()
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
			slog.Info("close error", "err", err)
		}
	}(conn)

	cx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel() // 断开时自动 cancel
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	modelAPIKey := strings.TrimSpace(os.Getenv("MODEL_API_KEY"))
	if modelAPIKey == "" {
		modelAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if modelAPIKey == "" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("<ZU1svmzfSE7zOyk>"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("后端未配置 MODEL_API_KEY 或 OPENAI_API_KEY，无法连接 AI 模型。"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte("</ZU1svmzfSE7zOyk>"))
		return
	}
	modelName := strings.TrimSpace(os.Getenv("MODEL_NAME"))
	if modelName == "" {
		modelName = "qwen3.6-plus"
	}
	modelBaseURL := strings.TrimSpace(os.Getenv("MODEL_BASE_URL"))
	client := openai.NewClient(option.WithAPIKey(modelAPIKey))
	if modelBaseURL != "" {
		client = openai.NewClient(option.WithAPIKey(modelAPIKey), option.WithBaseURL(modelBaseURL))
	}
	ctx := context.Background()
	tools := []any{
		map[string]any{
			"type": "web_search",
		}, //map[string]any{
		//	"type": "web_extractor",
		//},
		map[string]any{
			"type": "code_interpreter",
		},
		responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        "read_file",
				Description: openai.String("Read content from a absolute path of a local file. Returns an error message if the file does not exist or cannot be read."),
				Parameters: openai.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{
							"type":        "string",
							"description": "Absolute path to the file to read",
						},
					},
					"required": []string{"file_path"},
				},
			},
		},
	}
	var stream *ssestream.Stream[responses.ResponseStreamEventUnion]
	stream = client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: modelName,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(buildSystemPrompt(getPWD())),
		},
		Store:             openai.Bool(true),
		ParallelToolCalls: openai.Bool(false),
	}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))

	called := false
	for {
		if qwenGeneration.Load() != generation {
			return
		}
		if err != nil {
			slog.Info("new stream error", "err", err)
			return
		}
		if !called {
			err = conn.WriteMessage(websocket.TextMessage, []byte("<ZU1svmzfSE7zOyk>"))
			if err != nil {
				slog.Info("write error", "err", err)
				return
			}
		} else {
			called = false
		}
		for stream.Next() {
			event := stream.Current()
			// 普通文本输出直接转发给 websocket
			err = conn.WriteMessage(websocket.TextMessage, []byte(event.Delta))
			if err != nil {
				slog.Info("write error", "err", err)
				return
			}
		}
		event := stream.Current()
		slog.Debug("event", "json", event.RawJSON())
		if err := stream.Err(); err != nil {
			slog.Info("stream error", "err", err)
			return
		}
		eid := event.Response.ID
		storePreviousResponseID(eid)

		// 检查是否为 function_call 事件
		// openai-go v3 中，function_call 通常在 ResponseFunctionToolCall 类型中
		// 需要通过 event 的 Type 字段或输出项判断
		// 方式1: 如果是 completed 事件，遍历 resp.Output
		// 方式2: 如果是 streaming，检查 event 是否包含 function_call 增量
		// 这里假设你使用的是 Responses API 的 streaming，需要检查事件类型
		// 如果是 function_call 相关的流事件，提取并执行
		// 注意：Responses API 的 streaming 中，function_call 参数是逐步累积的
		// 建议在 ResponseCompletedEvent 时统一处理，或使用内部缓冲累积 arguments
		// 简化方案：先不处理 streaming 中的 function_call 增量，
		// 等收到完整响应后（可通过非 streaming 方式，或累积到 event 包含完整调用）再执行
		// 这里提供一个通用的处理逻辑框架：

		for _, item := range event.Response.Output {
			// 如果是 function_call 完成事件
			if item.Type == "function_call" {
				// 解析 function_call 参数
				// 注意：event 结构需根据实际响应调整，以下为示例
				var funcCall struct {
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"` // JSON string
				}
				funcCall.Name = item.Name
				funcCall.CallID = item.CallID
				funcCall.Arguments = item.Arguments
				if funcCall.Name == "read_file" {
					// 解析参数
					var args struct {
						FilePath string `json:"file_path"`
					}
					if err := json.Unmarshal([]byte(funcCall.Arguments), &args); err != nil {
						slog.Info("parse file_path error", "err", err)
						continue
					}

					// 执行文件读取
					content, err := os.ReadFile(args.FilePath)
					var result string
					if err != nil {
						result = fmt.Sprintf("Error reading file: %v", err)
					} else {
						// 可选：限制返回内容长度
						if len(content) > 100*1024 {
							result = string(content[:100*1024]) + "\n...[truncated]"
						} else {
							result = string(content)
						}
					}

					// 用 streaming 方式提交 tool output
					stream = client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
						Model:              modelName,
						PreviousResponseID: openai.String(previousResponseID()), // 当前 response ID
						Input: responses.ResponseNewParamsInputUnion{
							OfInputItemList: responses.ResponseInputParam{
								responses.ResponseInputItemParamOfFunctionCallOutput(funcCall.CallID, result),
							},
						},
						Store:             openai.Bool(true),
						ParallelToolCalls: openai.Bool(false),
					}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))

					if err := stream.Err(); err != nil {
						slog.Info("stream error", "err", err)
						return
					}
					called = true
					break
				} else {
					slog.Info("unknown function call", "name", funcCall.Name)
					continue
				}
			}
		}
		if called {
			continue
		}

		err = conn.WriteMessage(websocket.TextMessage, []byte("</ZU1svmzfSE7zOyk>"))
		if err != nil {
			slog.Info("write error", "err", err)
			return
		}
		var request string
		select {
		case r := <-requests:
			request = r
		case <-cx.Done():
			return
		}
		stream = client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
			Model:              modelName,
			PreviousResponseID: openai.String(previousResponseID()),
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String(request),
			},
			Store:             openai.Bool(true),
			ParallelToolCalls: openai.Bool(false),
		}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))
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
