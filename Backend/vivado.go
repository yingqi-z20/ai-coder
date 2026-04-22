package main

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func Echo(c *gin.Context) {
	conn, err := Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Info("upgrade error:", err)
		return
	}
	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {
			slog.Info("close error:", err)
		}
	}(conn)
	cmd := exec.Command("/tools/Xilinx/Vivado/2024.2/bin/vivado", "-mode", "tcl")
	cmd.Dir = PWD
	cmd.WaitDelay = time.Second
	tty, err := pty.Start(cmd)
	if err != nil {
		slog.Info("start error:", err)
		return
	}
	defer func() {
		err := tty.Close()
		if err != nil {
			slog.Info("tty close error:", err)
		}
		time.Sleep(time.Second)
		err = cmd.Process.Kill()
		if err != nil {
			slog.Info("kill error:", err)
		}
		err = cmd.Wait()
		if err != nil {
			slog.Info("wait error:", err)
		}
	}()
	var valid atomic.Bool
	valid.Store(true)
	_, proj := path.Split(PWD)
	_, err = tty.Write([]byte("cd " + proj + ".xpr\n"))
	if err != nil {
		slog.Info("write pipe error:", err)
		valid.Store(false)
	}
	go func() {
		message := make([]byte, 1048576)
		for valid.Load() {
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
			err = tty.SetReadDeadline(time.Now().Add(time.Second))
			if err != nil {
				slog.Info("set deadline error:", err)
				valid.Store(false)
			}
			n, err := tty.Read(message)
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			if err != nil {
				slog.Info("stdout pipe error:", err)
				valid.Store(false)
			}
			err = conn.WriteMessage(websocket.TextMessage, message[0:n])
			if err != nil {
				slog.Info("write error:", err)
				valid.Store(false)
			}
		}
		err = conn.Close()
		if err != nil {
			slog.Info("close error:", err)
		}
	}()
	for valid.Load() {
		mt, message, err := conn.ReadMessage()
		if err != nil {
			slog.Info("read error:", err)
			valid.Store(false)
			err := tty.Close()
			if err != nil {
				slog.Info("tty close error:", err)
			}
			break
		}
		if mt == websocket.PingMessage || mt == websocket.PongMessage {
			continue
		}
		if mt != websocket.TextMessage {
			slog.Info("message type error:", mt)
			break
		}
		// slog.Info("recv:", message)
		_, err = tty.Write(message)
		if err != nil {
			slog.Info("write pipe error:", err)
			valid.Store(false)
			break
		}
	}
}
