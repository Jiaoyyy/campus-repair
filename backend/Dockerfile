FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && CGO_ENABLED=0 go build -o /out/worker ./cmd/worker
FROM alpine:3.22
RUN adduser -D -u 10001 app && mkdir -p /data/uploads && chown app:app /data/uploads && chmod 700 /data/uploads
WORKDIR /app
COPY --from=build /out/ /app/
USER app
EXPOSE 8080
CMD ["/app/api"]
