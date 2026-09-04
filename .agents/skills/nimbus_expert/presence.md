# Presence

WebSocket channels that track *who* is currently in them — the "3 people are
viewing this document" primitive.

## Hub

```go
hub := presence.NewHub(presence.Config{ ... })
app.Router.Mount("/presence", http.HandlerFunc(hub.HandleWebSocket))
```

| Method | Purpose |
| --- | --- |
| `GetChannel(name) *Channel` | Get or create a channel |
| `Channels() []string` | Every active channel |
| `Broadcast(channel, Event)` | Send to everyone in a channel |
| `BroadcastExcept(channel, Event, exceptUserID)` | Send to everyone but one user |
| `UsersIn(channel) []User` | Who is present |
| `UserCount(channel) int` | How many |
| `HandleWebSocket(w, r)` | The upgrade handler |

## Channel

```go
ch := hub.GetChannel("doc:42")
ch.Broadcast(presence.Event{ ... })
users := ch.Users()
n := ch.Count()
```

`Join(client)` and `Leave(client)` are driven by the hub's WebSocket handler;
call them directly only if you are managing clients yourself.

`BroadcastExcept(event, exceptUserID)` is what you want for "someone else is
typing" — the originator should not receive their own event.

## As a plugin

```go
app.RegisterPlugin(presence.NewPlugin(cfg))
```

`PresencePlugin` implements `Register`, `Boot`, and `RegisterRoutes`.

## Guidance

1. Presence is **ephemeral**. A hub is process-local: with more than one
   instance, each sees only its own connections. Put a shared backplane
   (Redis pub/sub, [reverb](reverb_plugin.md)) behind it before scaling out.
2. Authenticate at the upgrade, not after — an unauthenticated socket that has
   already joined a channel has already leaked the member list.
3. Treat a disconnect as a leave, but expect duplicates: browsers reconnect, so
   the same user id can be present twice.

Related: [websocket](websocket.md) for plain sockets, [transmit](transmit_plugin.md)
for one-way SSE push.
