//go:build integration

package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

type echoHandler struct{}

func (h *echoHandler) HandleMessage(ctx context.Context, client *Client, msg Message) {
	client.Send <- msg
}

func (h *echoHandler) Unregister(client *Client) {}

func startBenchServer() (*httptest.Server, string) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			Conn:          conn,
			Send:          make(chan Message, 256),
			PlayerID:      "bench-player",
			WSHandHandler: &echoHandler{},
		}
		go client.WritePump()
		go client.ReadPump()
	})

	server := httptest.NewServer(handler)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

func BenchmarkWSConnect(b *testing.B) {
	server, wsURL := startBenchServer()
	defer server.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}
		conn.Close()
	}
}

func BenchmarkWSMessageRoundtrip(b *testing.B) {
	server, wsURL := startBenchServer()
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	msg := Message{Event: "ping", Data: json.RawMessage(`{"ts":1}`)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(msg)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			b.Fatalf("write failed: %v", err)
		}
		_, _, err := conn.ReadMessage()
		if err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}
