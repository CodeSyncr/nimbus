# CLI - Nimbus

The Nimbus CLI is the primary tool for scaffolding, code generation, and project management.

## Core Commands

### Project Management
-   `nimbus new`: Scaffold a new Nimbus project.
-   `nimbus serve`: Start the development server with hot reload.
-   `nimbus repl`: Open an interactive REPL for the application container.

### Code Generators
-   `make:model`, `make:controller`, `make:migration`, `make:middleware`, `make:job`, `make:validator`, `make:command`, `make:plugin`.

### Database
-   `db:create`, `db:migrate`, `db:rollback`, `db:seed`.

### Background Services
-   `queue:work`: Start processing queue jobs.
-   `schedule:run`: Run the task scheduler.
-   `schedule:list`: List all registered scheduled tasks.

### AI Copilot
-   `nimbus ai "<description>"`: Generate code from a natural language prompt.
-   `nimbus test:generate`: Automatically generate tests for your controllers.

## Plugin Management

-   `plugin:install [name]`: Installs a plugin and automatically patches `bin/server.go`, `config/config.go`, and `.env.example`.
-   `plugin:list`: Shows all available and installed plugins.

## Creating Custom Commands

Use `nimbus make:command` to create your own CLI tools within your application.

## Best Practices

1.  **Use Generators**: Save time and ensure consistency by using the `make:*` commands.
2.  **AI Scaffolding**: Use `nimbus ai` for complex initial boilerplate generation.
3.  **Hot Reload**: Always use `nimbus serve` during development for the best DX.
