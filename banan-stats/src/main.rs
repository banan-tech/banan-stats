mod analyzer;
mod dashboard;
mod ingest;
mod store;
mod state;

use anyhow::Context;
use clap::Parser;
use std::net::SocketAddr;
use std::sync::Arc;

#[derive(Parser, Debug)]
#[command(name = "banan-stats")]
struct Args {
    #[arg(long, default_value = ":7070")]
    listen: String,
    #[arg(long, default_value = "clj_simple_stats.duckdb")]
    db_path: String,
}

#[tokio::main]
async fn main() -> Result<(), anyhow::Error> {
    let args = Args::parse();
    let store = Arc::new(store::Store::open(&args.db_path)?);
    let http_addr = normalize_listen_addr(&args.listen)?;

    let app_state = state::AppState { store: store.clone() };
    let http_app = dashboard::router(app_state.clone()).merge(ingest::router(app_state));
    let http_listener = tokio::net::TcpListener::bind(http_addr).await?;
    let http_server = axum::serve(http_listener, http_app).with_graceful_shutdown(shutdown_signal());

    println!("banan-stats listening: http={}", http_addr);

    let http_task = async { http_server.await.map_err(anyhow::Error::from) };
    tokio::try_join!(http_task)?;
    Ok(())
}

fn normalize_listen_addr(listen: &str) -> Result<SocketAddr, anyhow::Error> {
    if listen.starts_with(':') {
        let normalized = format!("0.0.0.0{}", listen);
        return normalized
            .parse()
            .with_context(|| format!("invalid listen address {}", listen));
    }
    listen
        .parse()
        .with_context(|| format!("invalid listen address {}", listen))
}

async fn shutdown_signal() {
    let _ = tokio::signal::ctrl_c().await;
}
