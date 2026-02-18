BINARY_NAME := $(shell grep '^APP_NAME' make.config | sed -E 's/APP_NAME[[:space:]]*=[[:space:]]*//g' || echo "unknown")
BINARY_VERSION := $(shell grep '^APP_VERSION' make.config | sed -E 's/APP_VERSION[[:space:]]*=[[:space:]]*//g' || echo "unknown")

RELEASES_DIST := ./releases

.PHONY: clean
clean:
	@rm -rf $(RELEASES_DIST)

.PHONY: build
build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -ldflags "-s -w" -o $(BINARY_NAME) .

# 用当前 Go 工具链编译 Kafka 插件并放入 plugins/，避免与主程序 Go 版本不一致导致加载失败
PLUGIN_KAFKA_DIR := ../watchman-kafka
PLUGINS_OUT := ./plugins

.PHONY: plugin-kafka
plugin-kafka:
	@mkdir -p $(PLUGINS_OUT)
	@cd $(PLUGIN_KAFKA_DIR) && GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -buildmode=plugin -o $(CURDIR)/$(PLUGINS_OUT)/watchman-kafka.so .
	@echo "plugin built: $(PLUGINS_OUT)/watchman-kafka.so"
