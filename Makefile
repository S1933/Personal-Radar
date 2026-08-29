.PHONY: build test lint run migrate collect briefing web docker-up docker-down

build:
	go build -o bin/radar ./cmd/radar

test:
	go test ./...

run: build
	./bin/radar run -config config/radar.yaml

web: build
	./bin/radar web -config config/radar.yaml

migrate: build
	./bin/radar migrate -config config/radar.yaml

collect: build
	./bin/radar collect -config config/radar.yaml

briefing: build
	./bin/radar briefing -config config/radar.yaml

docker-up:
	docker compose -f deploy/docker-compose.yml --env-file .env up -d --build

docker-down:
	docker compose -f deploy/docker-compose.yml down
