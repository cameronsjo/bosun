#!/usr/bin/env bash
# Remove background from images using rembg in Docker.
# Usage: ./scripts/remove-bg.sh input.png [output.png]
#   If output is omitted, writes to input-nobg.png
#
# First run: docker build -t rembg -f scripts/Dockerfile.rembg scripts/

set -euo pipefail

INPUT="${1:?Usage: remove-bg.sh input.png [output.png]}"
INPUT_ABS="$(cd "$(dirname "$INPUT")" && pwd)/$(basename "$INPUT")"
INPUT_DIR="$(dirname "$INPUT_ABS")"
INPUT_FILE="$(basename "$INPUT_ABS")"

if [ -n "${2:-}" ]; then
  OUTPUT_ABS="$(cd "$(dirname "$2")" 2>/dev/null && pwd)/$(basename "$2")" 2>/dev/null || OUTPUT_ABS="$(pwd)/$2"
  OUTPUT_DIR="$(dirname "$OUTPUT_ABS")"
  OUTPUT_FILE="$(basename "$OUTPUT_ABS")"
else
  OUTPUT_DIR="$INPUT_DIR"
  OUTPUT_FILE="${INPUT_FILE%.*}-nobg.png"
  OUTPUT_ABS="$OUTPUT_DIR/$OUTPUT_FILE"
fi

echo "Removing background: $INPUT_FILE -> $OUTPUT_FILE"

docker run --rm \
  -v "$INPUT_DIR:/input:ro" \
  -v "$OUTPUT_DIR:/output" \
  rembg \
  "/input/$INPUT_FILE" "/output/$OUTPUT_FILE"

echo "Done: $OUTPUT_ABS"
