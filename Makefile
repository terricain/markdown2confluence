PLATFORMS := darwin/386 linux/386 linux/amd64 linux/arm64 linux/arm windows/amd64 darwin/amd64
SIGNED_PLATFORMS := darwin/amd64

temp = $(subst /, ,$@)
os = $(word 1, $(temp))
arch = $(word 2, $(temp))
now = $(shell date +'%Y-%m-%dT%T')
version = $(word 3, $(subst /, ,${GITHUB_REF}))

build:
	go build -ldflags="-s -w -X main.sha1=${GITHUB_SHA} -X main.buildTime=${now} -X main.version=${version}"

build_amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s -X main.sha1=${GITHUB_SHA} -X main.buildTime=${now} -X main.version=${version}" -o markdown2confluence

lint:
	go fmt ./...
	golangci-lint run

release: $(PLATFORMS)
mac_release: $(SIGNED_PLATFORMS)


$(PLATFORMS):
	GOOS=$(os) GOARCH=$(arch) go build -ldflags="-s -w -X main.sha1=${GITHUB_SHA} -X main.buildTime=${now} -X main.version=${version}" -o 'markdown2confluence-$(os)-$(arch)'
	chmod +x 'markdown2confluence-$(os)-$(arch)'

$(SIGNED_PLATFORMS):
	GOOS=$(os) GOARCH=$(arch) go build -ldflags="-s -w -X main.sha1=${GITHUB_SHA} -X main.buildTime=${now} -X main.version=${version}" -o 'markdown2confluence-$(os)-$(arch)'
	chmod +x 'markdown2confluence-$(os)-$(arch)'
	./gon gon_config.hcl
