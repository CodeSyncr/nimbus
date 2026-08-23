# Mail - Nimbus

Nimbus provides a unified mail API for sending emails across various providers.

## Core Features

-   **Unified API**: Send emails using the same method regardless of the underlying driver.
-   **Driver-Based Architecture**: Supports SMTP, Amazon SES, Mailgun, SendGrid, and more.
-   **Native API Support**: Drivers for SendGrid, Mailgun, and SES can communicate directly via HTTP APIs instead of SMTP.
-   **Queue Integration**: Send emails as background jobs with a single call.

## Usage

### Simple Email

```go
mail.Send(&mail.Message{
    To:      []string{"user@example.com"},
    Subject: "Welcome!",
    Body:    "<h1>Welcome!</h1>",
    HTML:    true,
})
```

### In-Process Mailing

Use for small, non-critical notifications. For anything substantial, use the `queue` integration.

## Best Practices

1.  **Queue Everything**: Transactional and bulk emails should ALWAYS be queued to avoid slowing down the user experience.
2.  **Use Drivers**: In production, prefer native API drivers over SMTP for better reliability and performance.
3.  **HTML Templates**: Use the `.nimbus` template engine to build branded, consistent email bodies.
4.  **Failure Logic**: Implement retry and circuit breaker patterns for critical notifications.
