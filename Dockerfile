FROM node:24-alpine AS frontend-build

WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS go-build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tradingcopilot ./cmd/tradingcopilot

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates curl tini
WORKDIR /app
ENV TC_ENV_FILE=/app/runtime/env/app.env \
    TC_FRONTEND_DIST=/app/frontend-dist
COPY --from=go-build /out/tradingcopilot /app/tradingcopilot
COPY --from=frontend-build /app/frontend/dist /app/frontend-dist
COPY docker/entrypoint.sh /app/docker/entrypoint.sh
RUN chmod +x /app/docker/entrypoint.sh && mkdir -p /app/runtime/env /app/data /app/logs
EXPOSE 8000
ENTRYPOINT ["/sbin/tini", "--", "/app/docker/entrypoint.sh"]
CMD ["all"]
