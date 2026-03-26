#!/usr/bin/env bash

set -euo pipefail

echo "==> npm audit (root production deps)"
npm audit --package-lock-only --omit=dev

echo
echo "==> npm audit (nova production deps)"
npm audit --prefix nova --package-lock-only --omit=dev
