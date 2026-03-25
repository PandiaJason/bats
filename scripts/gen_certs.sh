#!/bin/bash

CERT_DIR="certs"
mkdir -p $CERT_DIR

# Generate CA key and certificate
openssl genrsa -out $CERT_DIR/ca.key 2048
openssl req -x509 -new -nodes -key $CERT_DIR/ca.key -sha256 -days 365 -out $CERT_DIR/ca.crt -subj "/CN=BATS-CA"

# For each node (node1 to node4), generate a key and certificate
for i in {1..4}
do
    NODE="node$i"
    echo "Generating cert for $NODE..."
    openssl genrsa -out $CERT_DIR/$NODE.key 2048
    openssl req -new -key $CERT_DIR/$NODE.key -out $CERT_DIR/$NODE.csr -subj "/CN=$NODE"
    
    # Sign with CA (simplified, using SANs if needed, but CN is often enough for local testing)
    openssl x509 -req -in $CERT_DIR/$NODE.csr -CA $CERT_DIR/ca.crt -CAkey $CERT_DIR/ca.key -CAcreateserial -out $CERT_DIR/$NODE.crt -days 365 -sha256
done

echo "Certificates generated in $CERT_DIR"
