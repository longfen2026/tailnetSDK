# Tailnet SDK build targets.
#
# M0 (current): Go core + CLI, verified on every platform Go supports.
# M1..M4 add the language bindings below; the commands are listed so the
# toolchain requirements are explicit rather than implicit.

GO ?= go
DIST ?= dist

.PHONY: help
help:
	@echo "Core (available now):"
	@echo "  make build        - build every package"
	@echo "  make vet          - run go vet"
	@echo "  make test         - run unit tests"
	@echo "  make cli          - build the tailnetctl smoke-test CLI into $(DIST)"
	@echo "  make tidy         - tidy go.mod/go.sum"
	@echo ""
	@echo "Bindings (planned, see docs/PLAN.md):"
	@echo "  make c-shared     - M2: Windows/macOS desktop library"
	@echo "  make c-archive    - M2: static library for C/C++/Swift hosts"
	@echo "  make apple        - M4: iOS/macOS xcframework"
	@echo "  make android      - M3: Android AAR via gomobile bind"

.PHONY: build
build:
	$(GO) build ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: cli
cli:
	$(GO) build -o $(DIST)/tailnetctl ./cli/tailnetctl

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: smoke
smoke: cli
	@echo "Run the SDK against a real tailnet:"
	@echo "  $(DIST)/tailnetctl login -hostname tailnetsdk-dev"
	@echo "  $(DIST)/tailnetctl status"
	@echo "  $(DIST)/tailnetctl peers"
	@echo "  $(DIST)/tailnetctl listen -hostname tailnetsdk-a :8080   # terminal 1"
	@echo "  $(DIST)/tailnetctl dial -hostname tailnetsdk-b tailnetsdk-a:8080  # terminal 2"

# --- Bindings (implemented in later milestones) ---------------------------

.PHONY: c-shared
c-shared:
	$(GO) build -buildmode=c-shared -o $(DIST)/libtailnet.so ./bind/ffi

.PHONY: c-archive
c-archive:
	$(GO) build -buildmode=c-archive -o $(DIST)/libtailnet.a ./bind/ffi

# iOS device slice; requires macOS + Xcode (see libtailscale/swift/script for
# the clang wrappers used upstream).
.PHONY: apple
apple:
	GOOS=ios GOARCH=arm64 CGO_ENABLED=1 $(GO) build -tags ios -buildmode=c-archive \
		-o $(DIST)/libtailnet_ios.a ./bind/ffi

# Requires Android SDK/NDK and golang.org/x/mobile/cmd/gomobile.
.PHONY: android
android:
	gomobile bind -target android -androidapi 26 -o $(DIST)/libtailnet.aar ./bind/gomobile

.PHONY: clean
clean:
	rm -rf $(DIST)
