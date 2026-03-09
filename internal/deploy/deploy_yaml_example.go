package deploy

// DeployYAMLExample is the default deploy.yaml content for new apps.
const DeployYAMLExample = `# nimbus deploy config
# Run: nimbus deploy fly | railway | aws | docker

target: fly
app_name: my-app
# service: my-app   # Railway only; defaults to app_name
region: iad

# Run migrations before each deploy (Fly.io release_command)
migrations: true

# Env vars to set as secrets (from .env, never committed)
# secrets:
#   - DATABASE_URL
#   - REDIS_URL

# Worker processes (Fly.io process groups, Railway services)
# workers:
#   - command: queue:work
#     scale: 1
`
