export CGO_ENABLED := 0

SHA := $(shell git rev-parse --short=8 HEAD)
GITVERSION := $(shell git describe --long --all)
# gnu date format iso-8601 is parsable with Go RFC3339
BUILDDATE := $(shell date --iso-8601=seconds)
VERSION := $(or ${VERSION},$(shell git describe --tags --exact-match 2> /dev/null || git symbolic-ref -q --short HEAD || git rev-parse --short HEAD))

LINKMODE := -X 'github.com/metal-stack/v.Version=$(VERSION)' \
		    -X 'github.com/metal-stack/v.Revision=$(GITVERSION)' \
		    -X 'github.com/metal-stack/v.GitSHA1=$(SHA)' \
		    -X 'github.com/metal-stack/v.BuildDate=$(BUILDDATE)'


.PHONY: build
build:
	go build \
		-tags 'osusergo netgo' \
		-ldflags \
		"$(LINKMODE)" \
		-o bin/ip-announcer \
		./...
	strip bin/ip-announcer
