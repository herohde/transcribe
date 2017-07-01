
GOCMD ?= go
export GO_LDFLAGS = -ldflags "-X github.com/herohde/build.GitTree=`git rev-list HEAD | wc -l | tr -d '[[:space:]]'` -X github.com/herohde/build.GitHash=`git rev-parse --short HEAD`"

build:
	@$(GOCMD) build $(GO_LDFLAGS) ./...

install:
	@$(GOCMD) install $(GO_LDFLAGS) ./...
