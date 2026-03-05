.PHONY: build run watch clean

build:
	go build -o musicbot ./cmd/

run:
	go run ./cmd/

watch:
	@if [ -f ~/go/bin/air ]; then \
		~/go/bin/air; \
	else \
		echo "air is not installed. Installing..."; \
		go install github.com/air-verse/air@latest; \
		~/go/bin/air; \
	fi

clean:
	rm -f musicbot

install-tools:
	go install github.com/air-verse/air@latest
