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

### Nimbus Cloud Authentication
-   `nimbus login`: Authenticate with your Nimbus Cloud account (`https://nimbusgo.space`) via browser OAuth.
-   `nimbus logout`: Clear saved credentials.
-   `nimbus whoami`: Display logged-in account, email, and subscription tier.

### AI Copilot & Testing
-   `nimbus ai`: Launch Claude Code CLI-style interactive AI terminal workspace with slash commands (`/help`, `/clear`, `/context`, `/models`, `/routes`, `/offline`, `/exit`).
-   `nimbus ai "<prompt>"`: Query Nimbus Cloud AI with prompt, preview/apply file changes, and continue in interactive chat mode (pass `--offline` for local generation).
-   `nimbus test:generate` (`nimbus tg`): Automatically generate tests for your controllers via 100% offline static AST analysis.

## Plugin Management

-   `plugin:install [name]`: Installs a plugin and automatically patches `bin/server.go`, `config/config.go`, and `.env.example`.
-   `plugin:list`: Shows all available and installed plugins.

## Creating Custom Commands

Use `nimbus make:command` to create your own CLI tools within your application.

## Best Practices

1.  **Use Generators**: Save time and ensure consistency by using the `make:*` commands.
2.  **AI Scaffolding**: Use `nimbus ai` for complex initial boilerplate generation.
3.  **Hot Reload**: Always use `nimbus serve` during development for the best DX.
