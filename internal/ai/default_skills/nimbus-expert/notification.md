# Notification System

Multi-channel notification dispatch system for Nimbus applications.

## Notification Interface
```go
type Notification interface {
    Via() []string           // Channels: "mail", "database", "slack", etc.
    ToMail() *mail.Message   // Mail representation
    ToDatabase() map[string]any // Database representation
}
```

## Defining Notifications
```go
type OrderShipped struct {
    OrderID string
    User    auth.User
}

func (n *OrderShipped) Via() []string {
    return []string{"mail", "database"}
}

func (n *OrderShipped) ToMail() *mail.Message {
    return mail.NewMessage("Your order has shipped!").
        SetTo(n.User.Email).
        SetBody("Order " + n.OrderID + " is on its way.", false)
}

func (n *OrderShipped) ToDatabase() map[string]any {
    return map[string]any{
        "type":     "order_shipped",
        "order_id": n.OrderID,
    }
}
```

## Dispatching
```go
notification.Send(user, &OrderShipped{OrderID: "123", User: user})
```

## Channels
| Channel | Storage | Use Case |
|---------|---------|----------|
| `mail` | Email delivery | User-facing alerts |
| `database` | `notifications` table | In-app notification center |
| Custom | Implement `Channel` interface | Slack, SMS, webhooks |

## Database Channel
Notifications are stored in a `notifications` table with columns: `id`, `user_id`, `type`, `data` (JSON), `read_at`, `created_at`.
