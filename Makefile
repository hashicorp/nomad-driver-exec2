default: compile

NOMAD_PLUGIN_DIR ?= /tmp/nomad-plugins

.PHONY: clean
clean:
	@echo "==> Cleanup previous build"
	rm -f $(NOMAD_PLUGIN_DIR)/nomad-driver-exec2

.PHONY: compile
compile: clean
	@echo "==> Compile exec2 plugin"
	mkdir -p $(NOMAD_PLUGIN_DIR)
	go build -race -o $(NOMAD_PLUGIN_DIR)/nomad-driver-exec2

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
