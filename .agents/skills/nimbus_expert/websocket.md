# WebSocket Support

Nimbus provides WebSocket server support via the `websocket/` package (built on gorilla/websocket).

## Setup
```go
// In your routes
r.Get("/ws", websocket.Handler(onConnect))
```

## Connection Lifecycle
```go
func onConnect(conn *websocket.Conn) {
    defer conn.Close()
    
    for {
        msgType, data, err := conn.ReadMessage()
        if err != nil {
            break // Client disconnected
        }
        
        // Echo back
        conn.WriteMessage(msgType, data)
    }
}
```

## Channel-Based Messaging
```go
hub := websocket.NewHub()

// In route handler
r.Get("/ws", func(c *http.Context) error {
    return hub.HandleConnection(c, func(conn *websocket.Conn, msg []byte) {
        // Handle incoming message
        hub.Broadcast("room-1", msg) // Broadcast to room
    })
})
```

## Integration with Reverb Plugin
For production WebSocket broadcasting with Redis fan-out, use the Reverb plugin instead of raw WebSocket handlers:
```bash
nimbus plugin install reverb
```

## Key Types
- `websocket.Conn` — Single WebSocket connection
- `websocket.Hub` — Connection manager with rooms/channels
- `websocket.Handler` — HTTP upgrade handler
