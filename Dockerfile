FROM golang:1.23-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /gold-service .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN mkdir -p /data
COPY --from=build /gold-service /gold-service

EXPOSE 8080
ENTRYPOINT ["/gold-service"]
