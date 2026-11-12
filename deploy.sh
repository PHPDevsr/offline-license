#!/bin/sh
set -eu

cd "$(dirname "$0")"

docker compose build license-server
port=$(docker compose run --rm --no-deps -T license-server print-port)
LICENSE_COMPOSE_PORT="$port" docker compose up --detach --no-build --force-recreate license-server
