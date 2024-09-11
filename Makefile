TARGET ?= amiproxy

tidy:
	@go mod tidy

amiproxy: 
	go build -ldflags "-s -w" -o ${TARGET} 