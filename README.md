# inbox-collab
inbox-collab allows organizing and collaborating on emails that are sent to multiple people at once.
It solves the common problem in clubs where you never know who read and possibly responded to an email.

Mails from multiple configurable mail servers are collected
and based on reply headers and other metrics they are sorted into [matrix](https://matrix.org/) threads.
Boilerplate metadata, such as giant footers commonly found in emails, is stripped using a large language model.

## Installation
1. Clone the repository
2. Copy examples via `cp example.env .env && cp ./config/config.example.toml ./config/config.toml`
3. Edit configuration files accordingly
4. Use `docker compose run app --list-mailboxes` to determine valid mailbox values
5. Use `docker compose run app --verify-matrix` to automatically accept verifications requests; Log into the matrix account on another device and request verification
6. Run `docker compose up -d` to properly deploy

## Development
1. Enter nix shell with `nix-shell`
2. Run main go app with `go run ./cmd/main.go`
3. Run llm python app with `uv run fastapi dev`
4. Run database with `docker compose run -p 5432:5432 db`
5. Test go with `go test ./...`
6. Generate sqlc files with `sqlc generate`

