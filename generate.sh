#!/bin/bash

# Generate gRPC-Gateway code from proto files

# Install required tools if not present
if ! command -v protoc &> /dev/null; then
    echo "protoc not found. Please install Protocol Buffers compiler."
    exit 1
fi

if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "Installing protoc-gen-go-grpc..."
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

if ! command -v protoc-gen-grpc-gateway &> /dev/null; then
    echo "Installing protoc-gen-grpc-gateway..."
    go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
fi

if ! command -v protoc-gen-openapiv2 &> /dev/null; then
    echo "Installing protoc-gen-openapiv2..."
    go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest
fi

# Create directories
mkdir -p proto
mkdir -p third_party/google/api

# # Copy proto files
# cp ../proto/post/post.proto proto/
# cp -r ../third_party/google/api/* third_party/google/api/

# Generate Go code
echo "Generating Go code from proto files..."

# Generate gRPC service code
protoc -I . -I third_party \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/*/*.proto

# Generate gRPC-Gateway code
protoc -I . -I third_party \
    --grpc-gateway_out=. --grpc-gateway_opt=paths=source_relative \
    --grpc-gateway_opt=generate_unbound_methods=true \
    proto/*/*.proto

# Generate OpenAPI/Swagger documentation
protoc -I . -I third_party \
    --openapiv2_out=. --openapiv2_opt=logtostderr=true \
    proto/*/*.proto

echo "Code generation completed!"
# echo "Generated files:"
# echo "- proto/post/post.pb.go (protobuf messages)"
# echo "- proto/post/post_grpc.pb.go (gRPC service)"
# echo "- proto/post/post.pb.gw.go (gRPC-Gateway)"
# echo "- proto/post/post.swagger.json (OpenAPI/Swagger)"
