# Makefile

build:
	mkdir -p  ./bin
	go build -o 	bin/swechain-mcp-server 	src/swechain-mcp-server.go

deploy:
	mkdir -p  		~/.swechain-mcp-server/bin
	go build -o 	~/.swechain-mcp-server/bin/swechain-mcp-server 	src/swechain-mcp-server.go

# Clean target (if needed, to clean up)
clean:
	rm -rf ./bin/*
