pkgs := $(shell go list ./... | grep -v /types)
prod_pkgs := $(shell go list ./... | grep -v /types | grep -v /e2e)
files := $(shell find . -path ./vendor -prune -path ./types/types.pb.go -prune -o -name '*.go' -print)

.PHONY: all clean format test build vet lint checkformat check docker release-docker proto setup

all : install check
check : checkformat vet lint test
travis : check build docker

setup :
	@echo "== setup"
	go get github.com/golang/lint/golint
	go get golang.org/x/tools/cmd/goimports
	go get github.com/golang/dep/cmd/dep
	dep ensure

format :
	@echo "== format"
	@goimports -w $(files)
	@sync

clean :
	@echo "== clean"
	rm -rf build

build :
	@echo "== build"
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-s" -a -installsuffix static github.com/sky-uk/merlin/cmd/merlin
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-s" -a -installsuffix static github.com/sky-uk/merlin/cmd/meradm

install :
	@echo "== install"
	@go install -v ./cmd/...

unformatted = $(shell goimports -l $(files))

checkformat :
	@echo "== check formatting"
ifneq "$(unformatted)" ""
	@echo "needs formatting: $(unformatted)"
	@echo "run make format"
	@exit 1
endif

vet :
	@echo "== vet"
	@go vet $(prod_pkgs)

lint :
	@echo "== lint"
	@for pkg in $(prod_pkgs); do \
		golint -set_exit_status $$pkg || exit 1; \
	done;

test : install
	@echo "== run tests"
	@go test -race $(pkgs)

proto :
	@echo "== compiling proto files"
	docker run -v `pwd`/types:/types -w / grpc/go:1.0 protoc -I /types /types/types.proto --go_out=plugins=grpc:types

git_rev := $(shell git rev-parse --short HEAD)
git_tag := $(shell git tag --points-at=$(git_rev))
image := skycirrus/merlin

docker : build
	@echo "== build"
	docker build -t $(image):latest .

release-docker : docker
	@echo "== release"
ifeq ($(strip $(git_tag)),)
	@echo "no tag on $(git_rev), skipping release"
else
	@echo "releasing $(image):$(git_tag)"
	@docker login -u $(DOCKER_USERNAME) -p $(DOCKER_PASSWORD)
	docker tag $(image):latest $(image):$(git_tag)
	docker push $(image):$(git_tag)
endif
