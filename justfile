# enter dev shell
dev:
    nix develop

# generate database bindings
gen:
    sqlc generate

# run tests
test:
    go test ./...

# lint go and python
lint:
    nix run .#lint-go
    nix run .#lint-python

# build docker images
build:
    docker build .
    docker build . -f Dockerfile.llm

# run python app and llm tracing backend on http://localhost:6006
run-llm:
    uv run arize-phoenix serve 1>/dev/null &
    uv run fastapi dev

# manually run database in docker
run-db:
    docker compose run -p 5432:5432 db

# run go app (run after run-db and run-llm); make sure to set `python_api` to localhost
run *args:
    DATABASE_URL="${DATABASE_URL/@*:/@localhost:}" \
    go run ./cmd/main.go {{args}}

