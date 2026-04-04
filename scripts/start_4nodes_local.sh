#!/bin/bash
echo "Stopping any existing nodes..."
pkill -9 -f "bats-node" || true
pkill -9 -f "cmd/node/main.go" || true
rm -f /tmp/node*.log || true
sleep 1

echo "Building node binary..."
go build -o bats-node cmd/node/main.go

export PEERS=localhost:8001,localhost:8002,localhost:8003,localhost:8004

# Provide Node 1 with the Gemini Key so we have an active "Safety Brain"
export NODE1_AI_PROVIDER=google
export GOOGLE_API_KEY="${GEMINI_API_KEY}"

echo "Starting 4-node PBFT cluster..."
./bats-node node1 8001 > /tmp/node1.log 2>&1 &
./bats-node node2 8002 > /tmp/node2.log 2>&1 &
./bats-node node3 8003 > /tmp/node3.log 2>&1 &
./bats-node node4 8004 > /tmp/node4.log 2>&1 &

echo "Cluster is booting up. Waiting 3 seconds..."
sleep 3

echo "Cluster status:"
curl -k https://localhost:8001/status
echo ""
curl -k https://localhost:8002/status
echo ""
