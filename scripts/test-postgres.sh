#!/bin/sh
# Runs the Postgres integration tests (build tag pgintegration) against a
# throwaway postgres:16 container. The container is removed on exit, even when
# a test fails. Uses the docker CLI; Podman Desktop provides it.
# Usage: scripts/test-postgres.sh [go test args...]
set -eu

name="ocode-pg-test-$$"
port=55432
password="ocode-test"

cleanup() {
	docker rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run -d --name "$name" \
	-e POSTGRES_PASSWORD="$password" \
	-p "127.0.0.1:$port:5432" \
	postgres:16 >/dev/null

# The image starts a temporary server during first-time init. Wait for the
# final server on TCP, which the temporary one does not listen on.
i=0
until docker exec "$name" pg_isready -h 127.0.0.1 -q 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -ge 60 ]; then
		echo "postgres did not become ready within 60s" >&2
		exit 1
	fi
	sleep 1
done

OCODE_TEST_POSTGRES_URL="postgres://postgres:$password@127.0.0.1:$port/postgres?sslmode=disable" \
	go test -tags pgintegration -count=1 -timeout 10m -run 'TestPG' "$@" \
	./internal/dbconnect/ ./internal/server/
