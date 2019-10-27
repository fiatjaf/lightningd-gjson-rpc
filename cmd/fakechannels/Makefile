dist: $(shell find . -name "*.go")
	mkdir -p dist
	gox -tags="full" -osarch="darwin/amd64 linux/386 linux/amd64 linux/arm freebsd/amd64" -output="dist/fakechannels_{{.OS}}_{{.Arch}}"
