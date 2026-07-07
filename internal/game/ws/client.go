package ws

import (
	"context"
	"encoding/json"
	"time"

	"battle-squad/internal/shared/observability"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

type Client struct {
	Conn          *websocket.Conn
	Send          chan Message
	PlayerID      string
	AccountID     string
	RoomID        string
	LobbyID       string
	WSHandHandler HandlerInterface
}

type HandlerInterface interface {
	HandleMessage(ctx context.Context, client *Client, msg Message)
	Unregister(client *Client)
}

func (c *Client) ReadPump() {
	defer func() {
		observability.ActiveConnections.Dec()
		c.WSHandHandler.Unregister(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, payload, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}

		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			observability.Log.Warn().Err(err).Msg("failed to parse client ws message payload")
			continue
		}

		ctx := context.Background()
		if msg.CorrelationID != "" {
			ctx = context.WithValue(ctx, observability.CorrelationIDKey, msg.CorrelationID)
		}
		ctx = context.WithValue(ctx, observability.PlayerIDKey, c.PlayerID)
		ctx = context.WithValue(ctx, "accountId", c.AccountID)

		c.WSHandHandler.HandleMessage(ctx, c, msg)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			payload, err := json.Marshal(msg)
			if err == nil {
				w.Write(payload)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
