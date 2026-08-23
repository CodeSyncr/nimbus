# Usage & Best Practices - Nimbus

To build high-quality applications with Nimbus, follow these best practices and development patterns.

## Project Structure

-   **controllers**: Keep your controllers thin. Move complex business logic to service or action classes.
-   **models**: Use `database.Model` or `nosql.Model` for consistency. Define clear relations and use GORM tags for validation.
-   **middleware**: Use middleware for cross-cutting concerns like authentication, logging, and CORS.

## Development Workflow

1.  **Use the CLI**: Leverage `nimbus make:controller`, `nimbus make:model`, and `nimbus make:migration` to scaffold your application.
2.  **Hot Reload**: Run your application with `nimbus serve` to benefit from automatic restarts on code changes.
3.  **Validation**: Always validate incoming request data using the `validation` package and struct tags.
4.  **Error Handling**: Use the `errors` package for consistent error reporting and custom HTTP status codes.

## AI Integration

1.  **Agent Specialization**: Create specialized agents for specific tasks (e.g., "Code Reviewer", "Support Assistant") rather than one large, general agent.
2.  **Tool Safety**: Implement strict validation and authorization in your AI tool handlers.
3.  **Prompt Versioning**: Maintain your prompt templates in a central location or uses the `ai.Template` system for versioning within your application.
4.  **Memory Scoping**: Use session-based or user-based keys for agent memory to ensure data privacy and contextual accuracy.

## Performance & Scalability

1.  **Database Connection Pooling**: Configure `MaxOpenConns` and `MaxIdleConns` in your database config for production environments.
2.  **Rate Limiting**: Apply rate limiting to all public and AI-intensive endpoints.
3.  **Caching**: Use the `cache` package to store frequently accessed data and reduce database load.
4.  **Background Jobs**: Offload long-running tasks (e.g., email sending, data processing) to the `queue` package.

## Security & Authentication

1.  **JWT vs. PASETO**: Use PASETO for new projects as it is more secure by default and avoids common JWT pitfalls like the "alg: none" attack.
2.  **Stateless when possible**: Use stateless tokens (JWT/PASETO) for mobile apps and distributed services to avoid database lookups on every request.
3.  **Rotate Secrets**: Always rotate your `AUTH_TOKEN_SECRET` regularly and store it in a secure environment variable, never in the code.
4.  **Environment Variables**: Never hardcode sensitive information. Use `.env` files and the `config` package.
5.  **CSRF/XSS Protection**: Enable the `CSRF` middleware and use the `.nimbus` template engine for automatic HTML escaping.
6.  **Policy-Based Authorization**: Use the `auth` policy system for all sensitive operations.
