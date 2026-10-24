.PHONY: install build test run watch migrate clean

install:
	cd web && npm ci

build:
	cd web && npm run build
	mkdir -p tmp
	go build -o ./tmp/wisppay ./cmd/wisppay

test:
	go test ./...
	cd web && npm run typecheck

run: build
	./tmp/wisppay

watch:
	cd web && npm run watch

migrate:
	go run ./cmd/wisppay -migrate

clean:
	rm -rf tmp web/dist/assets