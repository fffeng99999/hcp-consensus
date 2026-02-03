#!/bin/bash
set -e

echo "========================================"
echo "Consensus Algorithm Comparison"
echo "========================================"
echo ""
echo "Comparing: tPBFT vs Raft vs HotStuff"
echo ""

# Configuration paths
TPBFT_CONFIG="./configs/tpbft-config.toml"
RAFT_CONFIG="./configs/raft-config.toml"
HOTSTUFF_CONFIG="./configs/hotstuff-config.toml"

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Function to test consensus
test_consensus() {
    local name=$1
    local config=$2
    
    echo "${BLUE}Testing $name...${NC}"
    
    # Apply configuration
    if [ -f "$config" ]; then
        cp "$config" "./testnet/node0/config/config.toml"
        echo "  Config applied: $config"
    else
        echo "  ${YELLOW}Warning: Config not found, using default${NC}"
    fi
    
    # Restart node with new config
    docker-compose restart node0 > /dev/null 2>&1
    sleep 5
    
    # Run benchmark
    bash scripts/benchmark.sh | tail -n 20
    
    echo ""
}

# Test tPBFT
echo "${GREEN}1. Testing tPBFT (Trust-based PBFT)${NC}"
test_consensus "tPBFT" "$TPBFT_CONFIG"

# Test Raft-style
echo "${GREEN}2. Testing Raft-style Consensus${NC}"
test_consensus "Raft" "$RAFT_CONFIG"

# Test HotStuff
echo "${GREEN}3. Testing HotStuff-style BFT${NC}"
test_consensus "HotStuff" "$HOTSTUFF_CONFIG"

echo "========================================"
echo "${GREEN}✅ Comparison complete!${NC}"
echo "========================================"
echo ""
echo "Summary:"
echo "  - tPBFT: Optimized for high-frequency trading"
echo "  - Raft: Simplified consensus (CFT)"
echo "  - HotStuff: Linear message complexity"
echo ""
echo "Check individual benchmark_results_*.txt files for details"
