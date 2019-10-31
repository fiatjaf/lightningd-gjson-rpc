dist: $(shell find . -name "*.go")
	mkdir -p dist
	gox -osarch="darwin/amd64 linux/386 linux/amd64 linux/arm freebsd/amd64" -output="dist/notifierbot_{{.OS}}_{{.Arch}}"
