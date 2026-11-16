FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY notification-service ./notification-service
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /service ./notification-service

FROM alpine:3.20
RUN mkdir /data && chown 10001:10001 /data
COPY --from=build /service /service
USER 10001:10001
ENV PORT=8083 DB_PATH=/data/notifications.db
EXPOSE 8083
ENTRYPOINT ["/service"]
