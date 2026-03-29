#!/bin/bash
# gen-certs.sh — Generate mTLS certificates and Ed25519 identities for BATS cluster.
# Supports arbitrary node counts. Usage: ./scripts/gen-certs.sh [node_count]
set -e

NODE_COUNT=${1:-4}
CERT_DIR="certs"

if [ -f "$CERT_DIR/ca.crt" ] && [ -f "$CERT_DIR/node1.crt" ]; then
    echo "[gen-certs] Certificates already exist in $CERT_DIR/. Skipping."
    echo "            Delete $CERT_DIR/ and re-run to regenerate."
    exit 0
fi

mkdir -p "$CERT_DIR"
echo "[gen-certs] Generating Root CA..."

openssl genrsa -out "$CERT_DIR/ca.key" 2048 2>/dev/null
openssl req -x509 -new -nodes \
    -key "$CERT_DIR/ca.key" \
    -sha256 -days 3650 \
    -out "$CERT_DIR/ca.crt" \
    -subj "/CN=BATS-CA/O=Xs10s Research" 2>/dev/null

# Inline Go program for Ed25519 key generation (no external deps)
KEYGEN=$(mktemp /tmp/bats_keygen_XXXXXX.go)
cat > "$KEYGEN" <<'GOEOF'
package main
import (
    "crypto/ed25519"
    "os"
)
func main() {
    nodeID := os.Args[1]
    dir := os.Args[2]
    pub, priv, _ := ed25519.GenerateKey(nil)
    os.WriteFile(dir+"/"+nodeID+".pub", pub, 0644)
    os.WriteFile(dir+"/"+nodeID+".identity", priv, 0600)
}
GOEOF

for i in $(seq 1 "$NODE_COUNT"); do
    NODE="node$i"
    echo "[gen-certs] Generating TLS cert + Ed25519 identity for $NODE..."

    # SAN config for localhost + Docker service name
    SAN_CONF=$(mktemp /tmp/bats_san_XXXXXX.cnf)
    cat > "$SAN_CONF" <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
[req_dn]
CN = $NODE
[v3_req]
subjectAltName = DNS:$NODE,DNS:localhost,IP:127.0.0.1
EOF

    openssl genrsa -out "$CERT_DIR/$NODE.key" 2048 2>/dev/null
    openssl req -new \
        -key "$CERT_DIR/$NODE.key" \
        -out "$CERT_DIR/$NODE.csr" \
        -subj "/CN=$NODE" \
        -config "$SAN_CONF" 2>/dev/null
    openssl x509 -req \
        -in "$CERT_DIR/$NODE.csr" \
        -CA "$CERT_DIR/ca.crt" \
        -CAkey "$CERT_DIR/ca.key" \
        -CAcreateserial \
        -out "$CERT_DIR/$NODE.crt" \
        -days 3650 -sha256 \
        -extensions v3_req \
        -extfile "$SAN_CONF" 2>/dev/null

    # Ed25519 identity
    go run "$KEYGEN" "$NODE" "$CERT_DIR"

    rm -f "$SAN_CONF" "$CERT_DIR/$NODE.csr"
done

rm -f "$KEYGEN" "$CERT_DIR/ca.srl"
echo "[gen-certs] Done. Generated $NODE_COUNT node certificates in $CERT_DIR/"
