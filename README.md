# Arxveil

Arxveil is a self-hosted platform for monitoring and managing personal
infrastructure. A Rust agent reports machine state to a Go server, while a
Flutter desktop app gives you one place to view and operate your machines. It
starts with status and telemetry, then expands toward service and container
management, safe remote actions, alerts, and maintenance workflows.

## Architecture

- `app/` — Flutter desktop client for viewing machines.
- `server/` — Go HTTP and gRPC server backed by PostgreSQL.
- `agent/` — Rust service installed on managed machines.
- `proto/` — shared gRPC protocol between the agent and server.
- `infra/` — Docker Compose setup for local development.
- `tools/arxveil/` — small Go CLI for common development commands.

## Version 1 goal

An agent connects to the server, sends basic machine status, and the Flutter
app displays the machine list, online state, CPU, memory, disk, and uptime.

## Future direction

Arxveil is intended to grow into a self-hosted operations platform with
machine history and alerts, container and service visibility, safe remote
actions, agent updates, and role-based access.

## Local development

Install the development CLI once with `go -C tools/arxveil install .`, then
run `arxveil backend start` to start PostgreSQL, migrations, and the server.
