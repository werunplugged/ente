#!/usr/bin/env bash
# Build up_photos for a chosen environment.
#   ./build.sh prod          → flutter build apk --release  --flavor=production  + dart_defines/production.json
#   ./build.sh stage         → flutter build apk --release  --flavor=staging     + dart_defines/staging.json
#   ./build.sh dev           → flutter build apk --release  --flavor=development + dart_defines/development.json
#   ./build.sh dev   debug   → flutter build apk --debug    --flavor=development + dart_defines/development.json
set -euo pipefail

env="${1:-}"
mode="${2:-release}"

case "$env" in
    prod|production)   flavor=production;   dd=dart_defines/production.json ;;
    stage|staging)     flavor=staging;      dd=dart_defines/staging.json ;;
    dev|development)   flavor=development;  dd=dart_defines/development.json ;;
    *)
        echo "Usage: $0 {prod|stage|dev} [release|debug]" >&2
        exit 1
        ;;
esac

case "$mode" in
    release|debug) ;;
    *) echo "Mode must be 'release' or 'debug' (got '$mode')" >&2; exit 1 ;;
esac

exec flutter build apk "--$mode" --flavor="$flavor" -t lib/main.dart --dart-define-from-file="$dd"
