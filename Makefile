default: build

.PHONY: test
test:
	go test -mod=vendor -v ./... 

.PHONY: build
build:
	./scripts/build.sh
