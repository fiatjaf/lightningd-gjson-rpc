dist: $(shell find . -name "*.go") node_modules/spark-wallet
	go-bindata -prefix node_modules/spark-wallet/dist/www -o bindata.go node_modules/spark-wallet/dist/www/...
	mkdir -p dist
	gox -osarch="darwin/amd64 linux/386 linux/amd64 linux/arm freebsd/amd64" -output="dist/sparko_{{.OS}}_{{.Arch}}"

sparko: $(shell find . -name "*.go") node_modules/spark-wallet
	go-bindata -debug -prefix node_modules/spark-wallet/dist/www -o bindata.go node_modules/spark-wallet/dist/www/...
	go build -o ./sparko

node_modules/spark-wallet:
	npm install spark-wallet
