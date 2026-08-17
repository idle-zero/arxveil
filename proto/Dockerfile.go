# Generate Go protobuf and gRPC bindings without requiring host tooling.
#
# Run from the repository root:
# docker build --target output --file proto/Dockerfile.go --output type=local,dest=server/internal/gen .

FROM golang:1.26.5-alpine AS generate

RUN apk add --no-cache protobuf protobuf-dev

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

COPY proto /proto

RUN mkdir -p /out \
    && protoc -I /proto -I /usr/include \
      --go_out=paths=source_relative:/out \
      --go-grpc_out=paths=source_relative:/out \
      /proto/agent/v1/agent.proto

FROM scratch AS output

COPY --from=generate /out /
