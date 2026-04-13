package main

import (
	"context"
	"encoding/json"
	"fmt"
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

	cx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel() // 断开时自动 cancel
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	client := openai.NewClient(option.WithAPIKey("sk-8e9agRwhq0TFPagCHKBlHCUnFhnIBYJ8I3NWSRI0oaMepSCa"), option.WithBaseURL("http://10.128.8.22:3000/v1"))
	ctx := context.Background()
	tools := []any{
		map[string]any{
			"type": "web_search",
		},
		map[string]any{
			"type": "web_extractor",
		},
		map[string]any{
			"type": "code_interpreter",
		},
		responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        "read_file",
				Description: openai.String("Read content from a local file path (Docker environment, no security restrictions)"),
				Parameters: openai.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{
							"type":        "string",
							"description": "Absolute or relative path to the file to read",
						},
					},
					"required": []string{"file_path"},
				},
			},
		},
	}
	stream := client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: "qwen3.6-plus",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(buildSystemPrompt(PWD)),
		},
		Store: openai.Bool(true),
	}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))

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

			// 检查是否为 function_call 事件
			// openai-go v3 中，function_call 通常在 ResponseFunctionToolCall 类型中
			// 需要通过 event 的 Type 字段或输出项判断

			// 方式1: 如果是 completed 事件，遍历 resp.Output
			// 方式2: 如果是 streaming，检查 event 是否包含 function_call 增量

			// 这里假设你使用的是 Responses API 的 streaming，需要检查事件类型
			// 打印调试信息确认结构：
			// slog.Info("event debug", "raw", event.RawJSON())

			// 如果是 function_call 相关的流事件，提取并执行
			// 注意：Responses API 的 streaming 中，function_call 参数是逐步累积的
			// 建议在 ResponseCompletedEvent 时统一处理，或使用内部缓冲累积 arguments

			// 简化方案：先不处理 streaming 中的 function_call 增量，
			// 等收到完整响应后（可通过非 streaming 方式，或累积到 event 包含完整调用）再执行

			// 这里提供一个通用的处理逻辑框架：

			// 普通文本输出直接转发给 websocket
			if event.Type == "response.output_text.delta" || event.Delta != "" {
				err = conn.WriteMessage(websocket.TextMessage, []byte(event.Delta))
				if err != nil {
					slog.Info("write error:", err)
					return
				}
				continue
			}

			// 如果是 function_call 完成事件
			// 注意：具体事件名需根据实际 API 响应调整，常见为 "response.function_call_arguments.done"
			if strings.Contains(event.Type, "function_call") && strings.Contains(event.Type, "done") {
				// 解析 function_call 参数
				// 注意：event 结构需根据实际响应调整，以下为示例
				var funcCall struct {
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"` // JSON string
				}
				if err := json.Unmarshal([]byte(event.RawJSON()), &funcCall); err != nil {
					slog.Error("parse function_call error", "err", err)
					continue
				}

				if funcCall.Name == "read_file" {
					// 解析参数
					var args struct {
						FilePath string `json:"file_path"`
					}
					if err := json.Unmarshal([]byte(funcCall.Arguments), &args); err != nil {
						slog.Error("parse file_path error", "err", err)
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

					for stream.Next() {
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

					// 用 streaming 方式提交 tool output
					prid := stream.Current().Response.ID
					stream = client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
						Model:              "qwen3.6-plus",
						PreviousResponseID: openai.String(prid), // 当前 response ID
						Input: responses.ResponseNewParamsInputUnion{
							OfInputItemList: responses.ResponseInputParam{
								responses.ResponseInputItemParamOfFunctionCallOutput(funcCall.CallID, result),
							},
						},
						Store: openai.Bool(true),
					}, option.WithJSONSet("enable_search", true), option.WithJSONSet("enable_thinking", true), option.WithJSONSet("tools", tools))
				}
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
		var request string
		select {
		case r := <-requests:
			request = r
		case <-cx.Done():
			break
		}
		stream = client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
			Model:              "qwen3.6-plus",
			PreviousResponseID: openai.String(prid),
			Input: responses.ResponseNewParamsInputUnion{
				OfString: openai.String(request),
			},
			Store: openai.Bool(true),
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
