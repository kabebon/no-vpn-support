# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Настройка Go-модулей
COPY go.mod ./
RUN go mod download

# Копируем исходный код
COPY main.go ./

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -o support-portal main.go

# Production stage
FROM alpine:latest

WORKDIR /app

# Копируем скомпилированный бинарник
COPY --from=builder /app/support-portal .

# Копируем статику
COPY static/ ./static/

# Открываем порт
EXPOSE 8080

# Запускаем сервер
CMD ["./support-portal"]
