all: build install-local generate-docs

ARCH?= $$(go env GOARCH)
OS?= $$(go env GOOS)
VERSION := 0.1.0

build: 
	go build -o terraform-provider-simpleipam_v${VERSION} .

install-local:
	mkdir -p ~/.terraform.d/plugins/github.com/googlecloudplatform/simpleipam/${VERSION}/$(OS)_$(ARCH)
	cp terraform-provider-simpleipam_v${VERSION} ~/.terraform.d/plugins/github.com/googlecloudplatform/simpleipam/${VERSION}/$(OS)_$(ARCH)

generate-docs:
	go generate

init-local:
	rm -f .terraform.lock.hcl
	terraform init