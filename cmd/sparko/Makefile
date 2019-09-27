dist: $(shell find . -name "*.go") spark-wallet/client/dist
	go-bindata -prefix spark-wallet/client/dist-o bindata.go spark-wallet/client/dist/...
	mkdir -p dist
	gox -osarch="darwin/amd64 linux/386 linux/amd64 linux/arm freebsd/amd64" -output="dist/sparko_{{.OS}}_{{.Arch}}"

sparko: $(shell find . -name "*.go") spark-wallet/client/dist
	go-bindata -debug -prefix spark-wallet/client/dist -o bindata.go spark-wallet/client/dist/...
	go build -o ./sparko

spark-wallet/client/dist: $(shell find spark-wallet/client/src)
	git submodule update
	cd spark-wallet/client && pnpm install --shamefully-flatten --prefer-offline
	cd spark-wallet/client && npm run dist
