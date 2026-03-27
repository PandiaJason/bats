#!/bin/bash
export OPENAI_API_KEY=""
export ANTHROPIC_API_KEY=""
export GOOGLE_API_KEY=""

pkill -9 -f "cmd/node/main.go" || true
pkill -9 -f "main node" || true
rm -f /tmp/node*.log || true
sleep 1

echo "Starting BATS Node (Standalone Mode for Mock Agent Evaluation)..."
# Start Node 1 with 0 peers (F=0). It will be the leader and simulate the test.
PEERS="NONE" go run cmd/node/main.go node1 8001 > /tmp/node1.log 2>&1 &

echo "Waiting for node to initialize..."
sleep 2

echo "Running OpenClaw / n8n Agent Simulation..."
python3 scripts/simulate_agent.py > /tmp/agent_sim.log 2>&1

echo "Simulation complete. Shutting down node..."
pkill -9 -f "cmd/node/main.go" || true
pkill -9 -f "main node" || true
