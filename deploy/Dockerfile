FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /bin/radar ./cmd/radar

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata python3 py3-pip
# twscrape + its deps (httpx, etc.) installed system-wide in the venv-less manner.
RUN python3 -m pip install --no-cache-dir --break-system-packages twscrape
COPY --from=build /bin/radar /usr/local/bin/radar
COPY xscraper/ /app/xscraper/
COPY deploy/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
WORKDIR /app
# Default to the system python3 (twscrape installed there).
ENV X_PYTHON=/usr/bin/python3
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
# Default args are passed to entrypoint.sh; it ignores them and reads
# RADAR_CONFIG / positional args. Kept for backwards compat.
CMD ["run", "-config", "config/radar.yaml"]
