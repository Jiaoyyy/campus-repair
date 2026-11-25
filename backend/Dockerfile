FROM golang:1.26.11-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=local go mod download
COPY . .
RUN GOTOOLCHAIN=local CGO_ENABLED=0 go build -o /out/ ./cmd/api ./cmd/worker ./cmd/mcp
FROM alpine:3.22
RUN apk add --no-cache ffmpeg && adduser -D -u 10001 app && mkdir -p /data/uploads && chown app:app /data/uploads && chmod 700 /data/uploads
WORKDIR /app
COPY --from=build /out/ /app/
USER app
EXPOSE 8080
CMD ["/app/api"]
