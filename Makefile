.PHONY: build install init start stop clean logs status test benchmark

# Build variables
BUILD_DIR := build
BINARY := hcpd
CHAIN_ID := hcp-testnet
NODE_COUNT := 4

# Colors for output
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

all: build

###############################################################################
###                                  Build                                  ###
###############################################################################

build:
	@echo "$(GREEN)Building hcpd binary with RocksDB support...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@go build -tags rocksdb -o $(BUILD_DIR)/$(BINARY) ./cmd/hcpd
	@echo "$(GREEN)✅ Build complete: $(BUILD_DIR)/$(BINARY)$(NC)"

install: build
	@echo "$(GREEN)Installing hcpd...$(NC)"
	@cp $(BUILD_DIR)/$(BINARY) $(GOPATH)/bin/
	@echo "$(GREEN)✅ Installed to $(GOPATH)/bin/$(BINARY)$(NC)"

###############################################################################
###                              Testnet Setup                              ###
###############################################################################

init: build
	@echo "$(GREEN)Initializing $(NODE_COUNT)-node testnet...$(NC)"
	@bash scripts/init-testnet.sh $(NODE_COUNT) $(CHAIN_ID)
	@echo "$(GREEN)✅ Testnet initialized!$(NC)"
	@echo "$(YELLOW)Node directories created in ./testnet/$(NC)"

reset:
	@echo "$(YELLOW)Resetting testnet data...$(NC)"
	@rm -rf testnet/node*
	@echo "$(GREEN)✅ Testnet data cleared$(NC)"

###############################################################################
###                            Docker Operations                            ###
###############################################################################

start:
	@echo "$(GREEN)Starting HCP testnet nodes...$(NC)"
	@docker-compose up -d
	@echo "$(GREEN)✅ Nodes started!$(NC)"
	@echo ""
	@echo "$(YELLOW)RPC Endpoints:$(NC)"
	@echo "  Node 0: http://localhost:26657"
	@echo "  Node 1: http://localhost:26667"
	@echo "  Node 2: http://localhost:26677"
	@echo "  Node 3: http://localhost:26687"
	@echo ""
	@echo "$(YELLOW)Check status with: make status$(NC)"

stop:
	@echo "$(YELLOW)Stopping HCP testnet...$(NC)"
	@docker-compose down
	@echo "$(GREEN)✅ Nodes stopped$(NC)"

restart: stop start

logs:
	@echo "$(YELLOW)Showing logs (Ctrl+C to exit)...$(NC)"
	@docker-compose logs -f

logs-node0:
	@docker-compose logs -f node0

logs-node1:
	@docker-compose logs -f node1

logs-node2:
	@docker-compose logs -f node2

logs-node3:
	@docker-compose logs -f node3

###############################################################################
###                              Monitoring                                 ###
###############################################################################

status:
	@echo "$(GREEN)Checking node status...$(NC)"
	@echo ""
	@echo "$(YELLOW)Node 0:$(NC)"
	@curl -s http://localhost:26657/status | jq '.result.sync_info' || echo "$(RED)Node 0 not responding$(NC)"
	@echo ""
	@echo "$(YELLOW)Node 1:$(NC)"
	@curl -s http://localhost:26667/status | jq '.result.sync_info' || echo "$(RED)Node 1 not responding$(NC)"
	@echo ""
	@echo "$(YELLOW)Node 2:$(NC)"
	@curl -s http://localhost:26677/status | jq '.result.sync_info' || echo "$(RED)Node 2 not responding$(NC)"
	@echo ""
	@echo "$(YELLOW)Node 3:$(NC)"
	@curl -s http://localhost:26687/status | jq '.result.sync_info' || echo "$(RED)Node 3 not responding$(NC)"

netinfo:
	@echo "$(GREEN)Network information:$(NC)"
	@curl -s http://localhost:26657/net_info | jq

consensus:
	@echo "$(GREEN)Consensus state:$(NC)"
	@curl -s http://localhost:26657/consensus_state | jq

###############################################################################
###                              Testing                                    ###
###############################################################################

test:
	@echo "$(GREEN)Running tests...$(NC)"
	@go test -v ./...

benchmark:
	@echo "$(GREEN)Running consensus benchmark...$(NC)"
	@bash scripts/benchmark.sh

###############################################################################
###                              Cleanup                                    ###
###############################################################################

clean:
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	@rm -rf $(BUILD_DIR)
	@echo "$(GREEN)✅ Build artifacts cleaned$(NC)"

clean-all: stop clean reset
	@echo "$(GREEN)✅ Full cleanup complete$(NC)"

###############################################################################
###                                Help                                     ###
###############################################################################

help:
	@echo "HCP Consensus - Makefile Commands"
	@echo ""
	@echo "$(GREEN)Build:$(NC)"
	@echo "  make build      - Build hcpd binary"
	@echo "  make install    - Install to GOPATH"
	@echo ""
	@echo "$(GREEN)Testnet:$(NC)"
	@echo "  make init       - Initialize testnet"
	@echo "  make reset      - Clear testnet data"
	@echo ""
	@echo "$(GREEN)Docker:$(NC)"
	@echo "  make start      - Start all nodes"
	@echo "  make stop       - Stop all nodes"
	@echo "  make restart    - Restart all nodes"
	@echo "  make logs       - View all logs"
	@echo ""
	@echo "$(GREEN)Monitoring:$(NC)"
	@echo "  make status     - Check node status"
	@echo "  make netinfo    - Network information"
	@echo "  make consensus  - Consensus state"
	@echo ""
	@echo "$(GREEN)Testing:$(NC)"
	@echo "  make test       - Run tests"
	@echo "  make benchmark  - Benchmark consensus"
	@echo ""
	@echo "$(GREEN)Cleanup:$(NC)"
	@echo "  make clean      - Clean build"
	@echo "  make clean-all  - Full cleanup"
