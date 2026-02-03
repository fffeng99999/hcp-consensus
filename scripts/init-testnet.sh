#!/bin/bash
set -e

# Configuration
NODE_COUNT=${1:-4}
CHAIN_ID=${2:-hcp-testnet}
BINARY="./build/hcpd"
TESTNET_DIR="./testnet"

echo "========================================"
echo "HCP Testnet Initialization"
echo "========================================"
echo "Nodes: $NODE_COUNT"
echo "Chain ID: $CHAIN_ID"
echo ""

# Clean previous data
if [ -d "$TESTNET_DIR" ]; then
    echo "Cleaning previous testnet data..."
    rm -rf "$TESTNET_DIR"/node*
fi

mkdir -p "$TESTNET_DIR"

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "Error: Binary not found at $BINARY"
    echo "Please run 'make build' first"
    exit 1
fi

echo "Initializing nodes..."
echo ""

# Initialize nodes
for i in $(seq 0 $((NODE_COUNT-1))); do
    NODE_DIR="$TESTNET_DIR/node$i"
    NODE_MONIKER="node$i"
    
    echo "[Node $i] Initializing..."
    
    # Initialize node
    $BINARY init "$NODE_MONIKER" \
        --chain-id "$CHAIN_ID" \
        --home "$NODE_DIR" \
        --overwrite \
        2>&1 | grep -v "WARNING" || true
    
    # Create validator key
    $BINARY keys add "validator$i" \
        --home "$NODE_DIR" \
        --keyring-backend test \
        --output json \
        > "$NODE_DIR/validator_key.json" 2>&1
    
    VALIDATOR_ADDR=$(jq -r '.address' "$NODE_DIR/validator_key.json")
    echo "  Address: $VALIDATOR_ADDR"
    
    # Add genesis account
    $BINARY genesis add-genesis-account "$VALIDATOR_ADDR" \
        1000000000stake,1000000000token \
        --home "$NODE_DIR" \
        --keyring-backend test
    
    echo "  ✅ Node $i initialized"
    echo ""
done

# Generate genesis transactions
echo "Generating genesis transactions..."
for i in $(seq 0 $((NODE_COUNT-1))); do
    NODE_DIR="$TESTNET_DIR/node$i"
    
    $BINARY genesis gentx "validator$i" \
        100000000stake \
        --chain-id "$CHAIN_ID" \
        --home "$NODE_DIR" \
        --keyring-backend test \
        --moniker "node$i" \
        2>&1 | grep -v "WARNING" || true
    
    echo "  ✅ Genesis tx created for node$i"
done
echo ""

# Collect genesis transactions in node0
echo "Collecting genesis transactions..."
cp "$TESTNET_DIR"/node*/config/gentx/*.json "$TESTNET_DIR/node0/config/gentx/"
$BINARY genesis collect-gentxs --home "$TESTNET_DIR/node0" 2>&1 | grep -v "WARNING" || true
echo ""

# Copy genesis to all nodes
echo "Distributing genesis file..."
for i in $(seq 1 $((NODE_COUNT-1))); do
    cp "$TESTNET_DIR/node0/config/genesis.json" "$TESTNET_DIR/node$i/config/genesis.json"
    echo "  ✅ Copied to node$i"
done
echo ""

# Configure persistent peers
echo "Configuring network peers..."
NODE0_ID=$($BINARY tendermint show-node-id --home "$TESTNET_DIR/node0")
PEERS="$NODE0_ID@node0:26656"

for i in $(seq 1 $((NODE_COUNT-1))); do
    NODE_ID=$($BINARY tendermint show-node-id --home "$TESTNET_DIR/node$i")
    PEERS="$PEERS,$NODE_ID@node$i:26656"
done

for i in $(seq 0 $((NODE_COUNT-1))); do
    CONFIG_FILE="$TESTNET_DIR/node$i/config/config.toml"
    APP_CONFIG="$TESTNET_DIR/node$i/config/app.toml"
    
    # Update config.toml
    sed -i.bak "s/^persistent_peers = .*/persistent_peers = \"$PEERS\"/" "$CONFIG_FILE"
    sed -i.bak 's/^timeout_commit = .*/timeout_commit = "500ms"/' "$CONFIG_FILE"
    sed -i.bak 's/^timeout_propose = .*/timeout_propose = "1000ms"/' "$CONFIG_FILE"
    sed -i.bak 's/^timeout_prevote = .*/timeout_prevote = "500ms"/' "$CONFIG_FILE"
    sed -i.bak 's/^timeout_precommit = .*/timeout_precommit = "500ms"/' "$CONFIG_FILE"
    sed -i.bak 's/^create_empty_blocks = .*/create_empty_blocks = true/' "$CONFIG_FILE"
    sed -i.bak 's/^create_empty_blocks_interval = .*/create_empty_blocks_interval = "0s"/' "$CONFIG_FILE"
    
    # Enable API and gRPC
    sed -i.bak 's/^enable = false/enable = true/' "$APP_CONFIG"
    sed -i.bak 's/^swagger = false/swagger = true/' "$APP_CONFIG"
    
    rm "$CONFIG_FILE.bak" "$APP_CONFIG.bak" 2>/dev/null || true
    
    echo "  ✅ Configured node$i"
done
echo ""

echo "========================================"
echo "✅ Testnet initialization complete!"
echo "========================================"
echo ""
echo "Next steps:"
echo "  1. Start nodes: make start"
echo "  2. Check status: make status"
echo "  3. View logs: make logs"
echo ""
echo "Node RPC endpoints:"
for i in $(seq 0 $((NODE_COUNT-1))); do
    PORT=$((26657 + i*10))
    echo "  Node $i: http://localhost:$PORT"
done
echo ""
