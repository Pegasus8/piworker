MAKEFLAGS += --silent

build:
	echo "Compiling WebUI..."
	./webui/frontend/node_modules/.bin/vue-cli-service build
	echo "Cleaning previous Packr files..."
	packr2 clean
	echo "Done, executing Packr..."
	packr2
	echo "Done. Compiling..."
	go build
	echo "All tasks finished!"

run:
	echo "Compiling the WebUI..."
	./webui/frontend/node_modules/.bin/vue-cli-service build
	echo "Cleaning previous Packr files..."
	packr2 clean
	echo "Done, executing Packr..."
	packr2
	echo "Done. Running..."
	go run *.go
