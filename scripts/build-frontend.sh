#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND="$ROOT/frontend"
DEST="$ROOT/internal/web/dist"

cd "$FRONTEND"
npm run build

rm -rf "$DEST"
mkdir -p "$DEST"
cp -r "$FRONTEND/dist/." "$DEST/"
printf 'placeholder for go:embed; replaced by frontend build\n' > "$DEST/.gitkeep"
echo "frontend build copied to $DEST"
