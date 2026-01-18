# Этап сборки
FROM golang:1.21-alpine AS builder
WORKDIR /app
# Копируем файлы модуля
COPY go.mod ./
# Игнорируем проверку суммы и скачиваем зависимости
RUN go env -w GOPROXY=direct && go env -w GOSUMDB=off
RUN go mod download
# Копируем остальной код и собираем
COPY . .
RUN go build -o mybot main.go

# Этап запуска
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/mybot .
# Если нужны сертификаты для Telegram API
RUN apk --no-cache add ca-certificates
CMD ["./mybot"]
