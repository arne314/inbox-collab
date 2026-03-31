# inbox-collab
inbox-collab allows organizing and collaborating on emails that are sent to multiple people at once.
It solves the common problem in clubs where you never know who read and possibly responded to an email.

Mails from multiple configurable imap mail servers are collected
and based on reply headers and other metrics they are sorted into [matrix](https://matrix.org/) threads.
Boilerplate metadata, such as giant footers commonly found in emails,
is optionally stripped using a large language model.

## Features
- Control via `!commands` in Matrix
- Overview of all open Matrix threads in specific (configured) channels
- Reply to mails via smtp and have them stored in imap mailboxes
- Extensive thread sorting configuration
- Handling of forwarded and replied-to messages
- Use LLM from either Ollama or an OpenAI compatible endpoint
- Operation without an LLM possible; Redundant reply parts will (mostly) still be stripped

## Usage
- `!help` for command overview
- `!open`, `!close`, `!forceclose` threads (`!forceclose` won't reopen on mail reply)
- `!move <room substring>` to move a thread into another channel
- `!resendoverview` and `!resendoverviewall` to recreate overview messages
- `!reply` and `!send` replies using a configurable smtp server

## Installation
1. Clone the repository
2. Copy examples via `cp example.env .env && cp config/config.example.toml config/config.toml`
3. Edit the configuration files accordingly. Available options are explained in the sample configuration. Feel free to open an issue!
4. Use `docker compose run app --list-mailboxes` to determine valid mailbox values
5. Use `docker compose run app --verify-matrix` to automatically accept verifications requests; Log into the matrix account on another device and request verification
6. Run `docker compose up -d` to properly deploy

## Development
Run `nix develop` to enter the nix shell for development.
Run `just --list` to see all development specific commands.

