package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"whatsapptool/internal/db"
)

// EventType identifies the kind of event sent to connected agents.
type EventType string

const (
	EventNewMessage         EventType = "new_message"
	EventMessageStatus      EventType = "message_status"
	EventNewConversation    EventType = "new_conversation"
	EventConversationUpdate EventType = "conversation_update"
	EventCampaignProgress   EventType = "campaign_progress"
)

// Event is the JSON envelope broadcast to clients.
type Event struct {
	Type           EventType `json:"type"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Data           any       `json:"data"`
}

// client holds one connected WebSocket session.
type client struct {
	id       string
	conn     *websocket.Conn
	send     chan []byte
	convID   string // "" = subscribe to all conversations
	tenantID string // partitioned by tenant
}

// Hub manages all active WebSocket connections and routes events.
type Hub struct {
	mu       sync.RWMutex
	clients  map[string]*client
	upgrader websocket.Upgrader
}

func NewHub(baseURL string) *Hub {
	allowedHost := ""
	if u, err := url.Parse(baseURL); err == nil {
		allowedHost = u.Host
	}
	return &Hub{
		clients: make(map[string]*client),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // non-browser client
				}
				u, err := url.Parse(origin)
				if err != nil {
					return false
				}
				return u.Host == r.Host || (allowedHost != "" && u.Host == allowedHost)
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
		},
	}
}

// ServeWS upgrades an HTTP connection to WebSocket and registers the client.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}

	tenantID := db.TenantFromContext(r.Context())
	id := r.RemoteAddr + "/" + r.URL.String()
	c := &client{
		id:       id,
		conn:     conn,
		send:     make(chan []byte, 64),
		convID:   r.URL.Query().Get("conv"),
		tenantID: tenantID,
	}

	h.mu.Lock()
	h.clients[id] = c
	h.mu.Unlock()

	go c.writePump()
	c.readPump(h) // blocks until disconnect
}

// BroadcastConversation sends an event to all clients of the same tenant subscribed to convID.
func (h *Hub) BroadcastConversation(tenantID, convID string, event Event) {
	b, err := json.Marshal(event)
	if err != nil {
		log.Printf("ws marshal: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		if c.tenantID != tenantID {
			continue
		}
		if c.convID == "" || c.convID == convID {
			select {
			case c.send <- b:
			default:
				log.Printf("ws: client %s send buffer full, dropping event", c.id)
			}
		}
	}
}

// BroadcastAll sends an event to every connected client of the same tenant.
func (h *Hub) BroadcastAll(tenantID string, event Event) {
	h.BroadcastConversation(tenantID, "", event)
}

func (h *Hub) unregister(id string) {
	h.mu.Lock()
	if c, ok := h.clients[id]; ok {
		delete(h.clients, id)
		close(c.send)
	}
	h.mu.Unlock()
}

// writePump drains the send channel to the WebSocket connection.
func (c *client) writePump() {
	ticker := time.NewTicker(54 * time.Second) // keep-alive ping
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump consumes inbound messages.
func (c *client) readPump(h *Hub) {
	defer func() {
		h.unregister(c.id)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}
