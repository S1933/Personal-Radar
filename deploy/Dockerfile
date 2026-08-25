FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /bin/radar ./cmd/radar

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /bin/radar /usr/local/bin/radar
WORKDIR /app
ENTRYPOINT ["radar"]
CMD ["run", "-config", "config/radar.yaml"]
