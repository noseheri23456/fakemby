// Package ws provides bounded per-client queues and RFC6455 connections.
package ws

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"sync"
	"time"
)

type Event struct {
	MessageType string `json:"MessageType"`
	Data        any    `json:"Data"`
}
type client struct {
	conn *websocket.Conn
	user string
	send chan []byte
}
type Hub struct {
	mu      sync.Mutex
	clients map[*client]bool
	closed  bool
}

func New() *Hub { return &Hub{clients: make(map[*client]bool)} }
func (h *Hub) Broadcast(user, kind string, data any) {
	b, err := json.Marshal(Event{kind, data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if user != "" && c.user != user {
			continue
		}
		select {
		case c.send <- b:
		default:
			_ = c.conn.Close()
		}
	}
}
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		_ = c.conn.Close()
	}
}

// Serve upgrades only after the caller authenticates the user. Browser origins
// are checked against the configured list or the request host by the upgrader.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, user string, origins []string) {
	up := websocket.Upgrader{HandshakeTimeout: 5 * time.Second}
	if len(origins) > 0 {
		up.CheckOrigin = func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			for _, o := range origins {
				if o == origin {
					return true
				}
			}
			return false
		}
	}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, user: user, send: make(chan []byte, 32)}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.clients[c] = true
	h.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case b, ok := <-c.send:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if conn.WriteMessage(websocket.TextMessage, b) != nil {
					_ = conn.Close()
					return
				}
			case <-ticker.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)) != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	conn.SetReadLimit(64 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(65 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(65 * time.Second)) })
	for {
		_, b, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var e Event
		if json.Unmarshal(b, &e) == nil && e.MessageType == "KeepAlive" {
			h.Broadcast(user, "KeepAlive", nil)
		}
	}
	h.mu.Lock()
	delete(h.clients, c)
	close(c.send)
	h.mu.Unlock()
	_ = conn.Close()
	<-done
}
