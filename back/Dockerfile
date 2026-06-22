FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mal-api ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/refresh-catalog ./cmd/refresh-catalog \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lazy-worker ./cmd/lazy-worker

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
  && addgroup -S app \
  && adduser -S -G app app \
  && mkdir -p /data \
  && chown -R app:app /data

WORKDIR /app

COPY --from=build /out/mal-api /usr/local/bin/mal-api
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/refresh-catalog /usr/local/bin/refresh-catalog
COPY --from=build /out/lazy-worker /usr/local/bin/lazy-worker

USER app

EXPOSE 8080

ENTRYPOINT ["mal-api"]

