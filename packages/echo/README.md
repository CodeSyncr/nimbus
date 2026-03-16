# @nimbus/echo

Real-time client SDK for Nimbus Transmit (SSE channels). Works with the Transmit SSE plugin on the server.

## Installation

```bash
npm install @nimbus/echo
```

## Usage

```typescript
import { Echo } from '@nimbus/echo'

const echo = new Echo({
  baseURL: 'http://localhost:3333',
})

// Public channel
echo.channel('notifications')
  .listen('NewMessage', (data) => {
    console.log('New message:', data)
  })

// Private channel (requires auth)
echo.private('projects.1')
  .listen('RenderComplete', (data) => {
    console.log('Render done:', data)
  })

// Presence channel
echo.join('room.1')
  .here((users) => console.log('Online:', users))
  .joining((user) => console.log('Joined:', user))
  .leaving((user) => console.log('Left:', user))
  .listen('ChatMessage', (data) => console.log(data))

// Connection events
echo.onConnect(() => console.log('Connected!'))
echo.onDisconnect(() => console.log('Disconnected'))

// Leave a channel
echo.leave('notifications')

// Disconnect entirely
echo.disconnect()
```

## Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `baseURL` | `string` | — | Nimbus server URL |
| `path` | `string` | `__transmit` | Transmit route prefix |
| `bearerToken` | `string` | — | Bearer token for authenticated channels |
| `csrfToken` | `string` | — | CSRF token for POST requests |
| `autoReconnect` | `boolean` | `true` | Auto-reconnect on disconnect |
| `reconnectDelay` | `number` | `1000` | Reconnect delay in ms |
| `maxReconnectAttempts` | `number` | `Infinity` | Max reconnect attempts |
