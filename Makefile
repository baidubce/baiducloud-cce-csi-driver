# init project path
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output

# init command params
GO      := go
GOROOT  := $(GO_1_14_1_HOME)
GOPATH  := $(shell $(GO) env GOPATH)
GOMOD   := $(GO) mod
GOBUILD := $(GO) build
GOTEST  := $(GO) test -gcflags="-N -l"
GOPKGS  := $$($(GO) list ./...| grep -vE "vendor")
GIT_COMMIT := $(shell git rev-parse HEAD)
VERSION := v1.1.1

# test cover files
COVPROF := $(HOMEDIR)/covprof.out  # coverage profile
COVFUNC := $(HOMEDIR)/covfunc.txt  # coverage profile information for each function
COVHTML := $(HOMEDIR)/covhtml.html # HTML representation of coverage profile

# make, make all
all: prepare compile package

# make image-all
image-all: all build-csi-plugin-image

# set proxy env
set-env:
	$(GO) env -w GO111MODULE=on
	$(GO) env -w GONOPROXY=\*\*.baidu.com\*\*
	$(GO) env -w GOPROXY=https://goproxy.io
	$(GO) env -w GONOSUMDB=\*


#make prepare, download dependencies
prepare: gomod

gomod: set-env
	$(GOMOD) tidy

#make compile
compile: build

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags "-X main.DriverVersion=$(VERSION) -X main.DriverGitCommit=$(GIT_COMMIT)" -o $(HOMEDIR)/csi-plugin $(HOMEDIR)/cmd/

# make test, test your code
test: prepare test-case
test-case:
	$(GOTEST) -v -cover $(GOPKGS)

# make package
package: package-bin
package-bin:
	mkdir -p $(OUTDIR)
	mv csi-plugin  $(OUTDIR)/


build-csi-plugin-image:
	docker build -t registry.baidubce.com/cce-plugin-pro/cce-csi-plugin:$(VERSION) -f build/Dockerfile .

push-image:
	docker push registry.baidubce.com/cce-plugin-pro/cce-csi-plugin:$(VERSION)

# make clean
clean:
	$(GO) clean
	rm -rf $(OUTDIR)
	rm -rf $(HOMEDIR)/csi-plugin
	rm -rf $(GOPATH)/pkg/darwin_amd64

# avoid filename conflict and speed up build 
.PHONY: all prepare compile test package clean build
