FROM rust:1.91-bookworm AS build

WORKDIR /src

COPY banan-stats/Cargo.toml banan-stats/
COPY banan-stats/assets banan-stats/assets
COPY banan-stats/src banan-stats/src

RUN cargo build --release --manifest-path banan-stats/Cargo.toml

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /src/banan-stats/target/release/banan-stats /app/banan-stats

EXPOSE 7070
EXPOSE 7071
ENTRYPOINT ["/app/banan-stats"]
