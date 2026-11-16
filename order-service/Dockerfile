FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY order-service ./order-service
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /service ./order-service

FROM alpine:3.20
RUN mkdir /data && chown 10001:10001 /data
COPY --from=build /service /service
USER 10001:10001
ENV PORT=8082 DB_PATH=/data/orders.db
EXPOSE 8082
ENTRYPOINT ["/service"]
