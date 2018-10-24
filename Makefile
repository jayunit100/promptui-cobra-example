build:
	go build -o blackduckctl ./cmd
test:
	go test ./cmd
	go test ./pkg
