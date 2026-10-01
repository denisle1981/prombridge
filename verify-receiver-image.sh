#!/usr/bin/env sh
set -eu
IMAGE="${1:-prom-udp-receiver:0.2.1}"
echo "Checking $IMAGE"
docker inspect "$IMAGE" --format 'Entrypoint={{json .Config.Entrypoint}} Cmd={{json .Config.Cmd}}'
docker run --rm "$IMAGE" --help 2>&1 | grep -E -- '--udp-listen|--http-listen|--reassembly-timeout|--max-compressed-bytes|--max-uncompressed-bytes|--max-packet-bytes'
echo "Receiver image accepts the Helm chart flags."
