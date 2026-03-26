#!/bin/bash
mkdir -p certs

# 1. Generate Root CA
openssl genrsa -out certs/ca.key 2048
openssl req -x509 -new -nodes -key certs/ca.key -sha256 -days 365 -out certs/ca.crt -subj "/CN=BATS-CA"

# Create a temporary Go generator for Ed25519
cat > gen_identity.go <<EOF
package main
import (
    "crypto/ed25519"
    "os"
)
func main() {
    nodeID := os.Args[1]
    pub, priv, _ := ed25519.GenerateKey(nil)
    os.WriteFile("certs/"+nodeID+".pub", pub, 0644)
    os.WriteFile("certs/"+nodeID+".identity", priv, 0600)
}
EOF

for i in 1 2 3 4; do
    NODE="node$i"
    # 2. Generate TLS Certs
    openssl genrsa -out "certs/$NODE.key" 2048
    openssl req -new -key "certs/$NODE.key" -out "certs/$NODE.csr" -subj "/CN=$NODE"
    openssl x509 -req -in "certs/$NODE.csr" -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out "certs/$NODE.crt" -days 365 -sha256

    # 3. Generate Ed25519 Identity
    go run gen_identity.go "$NODE"
done

rm gen_identity.go
echo "Certificates and Ed25519 Identities generated in certs/"
