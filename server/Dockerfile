FROM golang:1.26.5-alpine AS build

WORKDIR /src/server

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/arxveil-server ./cmd/arxveil-server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S arxveil \
    && adduser -S -G arxveil arxveil

WORKDIR /app
COPY --from=build /out/arxveil-server ./arxveil-server

USER arxveil

EXPOSE 8080 9090

ENTRYPOINT ["/app/arxveil-server"]
