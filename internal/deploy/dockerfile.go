package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

var dockerfileTmpl = `# Nimbus Go app - multi-stage build
FROM golang:{{.GoVersion}}-alpine AS builder
WORKDIR /app

# Install build deps (for cgo/sqlite if needed)
RUN apk add --no-cache gcc musl-dev

# Copy go mod and source (vendor included if present for replace directives)
COPY go.mod go.sum ./
COPY . .
# Use vendor when present (deploy with replace => ../ fails remotely)
RUN if [ -d vendor ]; then \
    CGO_ENABLED=1 go build -mod=vendor -ldflags="-s -w" -o /app/server .; \
  else \
    go mod download && CGO_ENABLED=1 go build -ldflags="-s -w" -o /app/server .; \
  fi

# Minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/. .
RUN rm -rf vendor go.mod go.sum

# Nimbus uses PORT env (default 8080)
ENV PORT=8080
EXPOSE 8080

CMD ["./server"]
`

var dockerfileNoSumTmpl = `# Nimbus Go app - multi-stage build
FROM golang:{{.GoVersion}}-alpine AS builder
WORKDIR /app

# Install build deps (for cgo/sqlite if needed)
RUN apk add --no-cache gcc musl-dev

# Copy go mod and source (vendor included if present for replace directives)
COPY go.mod ./
COPY . .
RUN if [ -d vendor ]; then \
    CGO_ENABLED=1 go build -mod=vendor -ldflags="-s -w" -o /app/server .; \
  else \
    go mod download && CGO_ENABLED=1 go build -ldflags="-s -w" -o /app/server .; \
  fi

# Minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/. .
RUN rm -rf vendor go.mod go.sum

ENV PORT=8080
EXPOSE 8080

CMD ["./server"]
`

// EnsureDockerfile creates a Dockerfile if missing. Returns path.
func EnsureDockerfile(dir string) (string, error) {
	path := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	tmplStr := dockerfileTmpl
	if _, err := os.Stat(filepath.Join(dir, "go.sum")); err != nil {
		tmplStr = dockerfileNoSumTmpl
	}
	t, err := template.New("dockerfile").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := t.Execute(f, map[string]string{"GoVersion": DetectGoVersion(dir)}); err != nil {
		return "", err
	}
	fmt.Println("  Created Dockerfile")
	return path, nil
}

// DetectGoVersion reads go version from go.mod.
func DetectGoVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "1.26"
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(line[3:])
		}
	}
	return "1.26"
}
