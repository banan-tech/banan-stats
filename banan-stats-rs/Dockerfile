FROM rust:1.91-bookworm AS build

WORKDIR /src

COPY banan-stats-rs/Cargo.toml banan-stats-rs/
COPY banan-stats-rs/assets banan-stats-rs/assets
COPY banan-stats-rs/src banan-stats-rs/src

RUN cargo build --release --manifest-path banan-stats-rs/Cargo.toml

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /src/banan-stats-rs/target/release/banan-stats /app/banan-stats

EXPOSE 7070
EXPOSE 7071
ENTRYPOINT ["/app/banan-stats"]
