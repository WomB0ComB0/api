#!/bin/bash

# Generate TypeScript client from OpenAPI specs

set -e

echo "🔧 Generating API Clients"
echo "========================"
echo ""

# Create output directory
OUTPUT_DIR="generated"
mkdir -p "$OUTPUT_DIR"

# Generate Core API client
echo "📦 Generating Core API client..."
npx openapi-typescript-codegen \
  --input http://localhost/v1/core/openapi.json \
  --output "$OUTPUT_DIR/core-api" \
  --client fetch

echo "✅ Core API client generated"

# Generate Media API client
echo ""
echo "📦 Generating Media API client..."
npx openapi-typescript-codegen \
  --input http://localhost/v1/media/openapi.json \
  --output "$OUTPUT_DIR/media-api" \
  --client fetch

echo "✅ Media API client generated"

# Generate GraphQL types
echo ""
echo "📦 Generating GraphQL types..."
npx graphql-codegen --config codegen.yml

echo "✅ GraphQL types generated"

echo ""
echo "🎉 All clients generated successfully!"
echo "📁 Output directory: $OUTPUT_DIR"
