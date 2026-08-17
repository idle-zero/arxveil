# Protocol code generation

`agent/v1/agent.proto` is the source of truth for Arxveil's agent protocol.

Generate the committed Go bindings from the repository root with Docker:

```powershell
docker build --target output --file proto/Dockerfile.go --output type=local,dest=server/internal/gen .
```

After a protocol change, regenerate the bindings and run `go mod tidy` from
`server/` before committing the `.proto`, generated Go files, and module-file
changes together.
