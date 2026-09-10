# Build from the repository root:  docker build -f cmd/nimbus-tunnel/Dockerfile .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /nimbus-tunnel ./cmd/nimbus-tunnel

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /nimbus-tunnel /usr/local/bin/nimbus-tunnel
EXPOSE 8090
ENV TUNNEL_ADDR=:8090
ENTRYPOINT ["nimbus-tunnel"]
