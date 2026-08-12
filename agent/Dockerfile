FROM rust:1.97-bookworm AS build

WORKDIR /src

COPY agent/Cargo.toml agent/Cargo.lock /src/agent/
COPY agent/build.rs /src/agent/build.rs
COPY agent/src /src/agent/src
COPY proto /src/proto

WORKDIR /src/agent
RUN cargo build --release --locked

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install --no-install-recommends --yes ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system arxveil \
    && useradd --system --gid arxveil --home-dir /var/lib/arxveil --create-home \
      --shell /usr/sbin/nologin arxveil

COPY --from=build /src/agent/target/release/arxveil-agent /usr/local/bin/arxveil-agent

USER arxveil
ENTRYPOINT ["/usr/local/bin/arxveil-agent"]
