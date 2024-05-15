default: compile

NOMAD_PLUGIN_DIR ?= /tmp/nomad-plugins

.PHONY: clean
clean:
	@echo "==> Cleanup previous build"
	rm -f $(NOMAD_PLUGIN_DIR)/nomad-driver-exec2

.PHONY: copywrite
copywrite:
	@echo "==> Checking copywrite headers"
	copywrite --config .copywrite.hcl headers --spdx "MPL-2.0"

.PHONY: compile
compile: clean
	@echo "==> Compile exec2 plugin"
	mkdir -p $(NOMAD_PLUGIN_DIR)
	go build -race -trimpath -o $(NOMAD_PLUGIN_DIR)/nomad-driver-exec2

.PHONY: e2e
e2e:
	@echo "==> Run exec2 e2e tests"
	cd e2e && GOFLAGS='--tags=e2e' go test -v .

.PHONY: test
test:
	@echo "==> Run exec2 tests"
	go test -race -v ./...

.PHONY: vet
vet:
	@echo "==> Vet exec2 packages"
	go vet ./...

.PHONY: hack
hack: compile
hack:
	@echo "==> Run dev Nomad with exec2 plugin"
	sudo nomad agent -dev -plugin-dir=$(NOMAD_PLUGIN_DIR) -config=e2e/agent.hcl

# CRT release compilation
dist/%/nomad-driver-exec2: GO_OUT ?= $@
dist/%/nomad-driver-exec2:
	@echo "==> RELEASE BUILD of $@ ..."
	GOOS=linux GOARCH=$(lastword $(subst _, ,$*)) \
	go build -trimpath -o $(GO_OUT)

# CRT release packaging (zip only)
.PRECIOUS: dist/%/nomad-driver-exec2
dist/%.zip: dist/%/nomad-driver-exec2
	@echo "==> RELEASE PACKAGING of $@ ..."
	@cp LICENSE $(dir $<)LICENSE.txt
	zip -j $@ $(dir $<)*

# CRT version generation
.PHONY: version
version:
	@$(CURDIR)/version/generate.sh version/version.go version/version.go
