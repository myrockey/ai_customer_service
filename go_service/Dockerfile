FROM golang:1.25-alpine AS builder
WORKDIR /build
ENV GOPROXY=https://goproxy.cn,direct
COPY go_service/go.mod go_service/go.sum ./
RUN go mod download
COPY go_service/ .
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o go_service .

FROM alpine:3.24
RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /build/go_service ./
COPY go_service/config.yaml ./
EXPOSE 8080
CMD ["./go_service"]