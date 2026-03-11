# 构建阶段
FROM golang:1.23-alpine AS builder

WORKDIR /build
RUN apk add --no-cache git ca-certificates
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn
# 先复制依赖文件，利用缓存
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/gobackend .

# 运行阶段
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app
COPY --from=builder /app/gobackend .

# 默认端口（与 config 中 SERVER_PORT 一致）
EXPOSE 8080

# 运行时通过挂载提供 product.env / dev.env，或通过环境变量配置
CMD ["./gobackend"]
