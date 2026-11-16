FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY menu-service ./menu-service
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /service ./menu-service

FROM alpine:3.20
RUN mkdir /data && chown 10001:10001 /data
COPY --from=build /service /service
USER 10001:10001
ENV PORT=8081 DB_PATH=/data/menu.db
EXPOSE 8081
ENTRYPOINT ["/service"]
