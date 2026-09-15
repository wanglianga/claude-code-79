# ---------- 阶段 1：前端构建 ----------
FROM node:22-alpine AS fe
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---------- 阶段 2：后端构建 ----------
FROM golang:1.25-alpine AS be
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/*.go backend/schema.sql ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server .

# ---------- 阶段 3：运行时（非 root + 健康检查） ----------
FROM alpine:3.20
RUN apk add --no-cache curl tzdata ca-certificates \
  && addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=be /out/server /app/server
COPY --from=fe /src/dist /app/web
RUN mkdir -p /app/uploads && chown -R app:app /app
USER app
ENV PORT=8080 \
    WEB_DIR=/app/web \
    UPLOAD_DIR=/app/uploads \
    TZ=Asia/Shanghai
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
  CMD curl -fsS http://localhost:8080/api/health || exit 1
CMD ["/app/server"]
