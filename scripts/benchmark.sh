#!/bin/bash
set -e

echo "========================================"
echo "HCP Consensus Performance Benchmark"
echo "========================================"
echo ""

BINARY="./build/hcpd"
NODE0_HOME="./testnet/node0"
CHAIN_ID="hcp-testnet"
VALIDATOR="validator0"

# Test configuration
TX_COUNT=100
WARM_UP=10

# Colors
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "Configuration:"
echo "  Transactions: $TX_COUNT"
echo "  Warm-up: $WARM_UP"
echo ""

# Check if node is running
if ! curl -s http://localhost:26657/status > /dev/null 2>&1; then
    echo "Error: Node is not running"
    echo "Please start the testnet with: make start"
    exit 1
fi

echo "${GREEN}Node is running${NC}"
echo ""

# Function to send transaction and measure latency
send_tx() {
    local recipient="hcp1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0z0z0z"
    local start_time=$(date +%s%3N)
    
    $BINARY tx bank send "$VALIDATOR" "$recipient" 1stake \
        --from "$VALIDATOR" \
        --chain-id "$CHAIN_ID" \
        --home "$NODE0_HOME" \
        --keyring-backend test \
        --yes \
        --broadcast-mode sync \
        --output json \
        2>&1 | jq -r '.txhash' > /dev/null
    
    local end_time=$(date +%s%3N)
    local latency=$((end_time - start_time))
    echo $latency
}

# Warm-up
echo "${YELLOW}Warming up ($WARM_UP transactions)...${NC}"
for i in $(seq 1 $WARM_UP); do
    send_tx > /dev/null
    sleep 0.1
done
echo "${GREEN}✅ Warm-up complete${NC}"
echo ""

# Run benchmark
echo "${BLUE}Running benchmark ($TX_COUNT transactions)...${NC}"
echo ""

LATENCIES=()
SUCCESS_COUNT=0
FAIL_COUNT=0

for i in $(seq 1 $TX_COUNT); do
    LATENCY=$(send_tx 2>/dev/null || echo "0")
    
    if [ "$LATENCY" -gt 0 ]; then
        LATENCIES+=("$LATENCY")
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        
        if [ $((i % 10)) -eq 0 ]; then
            echo "  Progress: $i/$TX_COUNT transactions sent"
        fi
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    
    sleep 0.05
done

echo ""
echo "${GREEN}✅ Benchmark complete${NC}"
echo ""

# Calculate statistics
if [ ${#LATENCIES[@]} -gt 0 ]; then
    # Sort latencies
    IFS=$'\n' SORTED_LATENCIES=($(sort -n <<<"${LATENCIES[*]}"))
    unset IFS
    
    # Calculate metrics
    TOTAL=0
    for lat in "${LATENCIES[@]}"; do
        TOTAL=$((TOTAL + lat))
    done
    
    AVG=$((TOTAL / ${#LATENCIES[@]}))
    MIN=${SORTED_LATENCIES[0]}
    MAX=${SORTED_LATENCIES[-1]}
    
    # P50 (median)
    P50_IDX=$((${#SORTED_LATENCIES[@]} / 2))
    P50=${SORTED_LATENCIES[$P50_IDX]}
    
    # P99
    P99_IDX=$(((${#SORTED_LATENCIES[@]} * 99) / 100))
    P99=${SORTED_LATENCIES[$P99_IDX]}
    
    # Calculate TPS (transactions per second)
    TOTAL_TIME=$((TOTAL / 1000))  # Convert to seconds
    if [ $TOTAL_TIME -gt 0 ]; then
        TPS=$((SUCCESS_COUNT / TOTAL_TIME))
    else
        TPS=$SUCCESS_COUNT
    fi
    
    echo "========================================"
    echo "Benchmark Results"
    echo "========================================"
    echo ""
    echo "${GREEN}Transaction Stats:${NC}"
    echo "  Total Sent:    $TX_COUNT"
    echo "  Successful:    $SUCCESS_COUNT"
    echo "  Failed:        $FAIL_COUNT"
    echo "  Success Rate:  $((SUCCESS_COUNT * 100 / TX_COUNT))%"
    echo ""
    echo "${BLUE}Latency (milliseconds):${NC}"
    echo "  Average:       ${AVG}ms"
    echo "  Median (P50):  ${P50}ms"
    echo "  P99:           ${P99}ms"
    echo "  Min:           ${MIN}ms"
    echo "  Max:           ${MAX}ms"
    echo ""
    echo "${YELLOW}Throughput:${NC}"
    echo "  TPS:           ~${TPS} tx/s"
    echo ""
    echo "========================================"
    echo ""
    
    # Save results
    RESULTS_FILE="benchmark_results_$(date +%Y%m%d_%H%M%S).txt"
    {
        echo "HCP Consensus Benchmark Results"
        echo "Date: $(date)"
        echo ""
        echo "Transactions: $SUCCESS_COUNT/$TX_COUNT"
        echo "Average Latency: ${AVG}ms"
        echo "P50 Latency: ${P50}ms"
        echo "P99 Latency: ${P99}ms"
        echo "TPS: ~${TPS}"
    } > "$RESULTS_FILE"
    
    echo "${GREEN}Results saved to: $RESULTS_FILE${NC}"
else
    echo "${RED}Error: No successful transactions${NC}"
    exit 1
fi
