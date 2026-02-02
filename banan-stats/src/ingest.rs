use crate::analyzer::Line;
use crate::state::AppState;
use axum::{
    body::Body,
    extract::State,
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::post,
    Router,
};
use chrono::{DateTime, Utc};
use futures_util::StreamExt;
use http_body_util::BodyExt;
use serde::Deserialize;

pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/ingest", post(ingest_handler))
        .with_state(state)
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct IngestEvent {
    #[serde(default)]
    event_id: String,
    #[serde(default)]
    timestamp: Option<DateTime<Utc>>,
    #[serde(default)]
    host: String,
    #[serde(default)]
    path: String,
    #[serde(default)]
    query: String,
    #[serde(default)]
    ip: String,
    #[serde(default)]
    user_agent: String,
    #[serde(default)]
    referrer: String,
    #[serde(default)]
    content_type: String,
    #[serde(default)]
    set_cookie: String,
    #[serde(default)]
    uniq: String,
    #[serde(default)]
    second_visit: bool,
}

async fn ingest_handler(State(state): State<AppState>, body: Body) -> Response {
    match ingest_stream(state, body).await {
        Ok(()) => StatusCode::ACCEPTED.into_response(),
        Err(err) => {
            eprintln!("ingest failed: {}", err);
            StatusCode::BAD_REQUEST.into_response()
        }
    }
}

async fn ingest_stream(state: AppState, body: Body) -> Result<(), anyhow::Error> {
    let mut stream = body.into_data_stream();
    let mut buffer: Vec<u8> = Vec::new();
    let mut lines = Vec::new();

    while let Some(chunk) = stream.next().await {
        let bytes = chunk?;
        buffer.extend_from_slice(&bytes);
        while let Some(pos) = buffer.iter().position(|b| *b == b'\n') {
            let line = buffer.drain(..=pos).collect::<Vec<u8>>();
            let trimmed = line
                .iter()
                .filter(|b| **b != b'\n' && **b != b'\r')
                .copied()
                .collect::<Vec<u8>>();
            if trimmed.is_empty() {
                continue;
            }
            let evt: IngestEvent = serde_json::from_slice(&trimmed)?;
            lines.push(event_to_line(evt));
        }
    }

    if !buffer.is_empty() {
        let trimmed = buffer
            .iter()
            .filter(|b| **b != b'\n' && **b != b'\r')
            .copied()
            .collect::<Vec<u8>>();
        if !trimmed.is_empty() {
            let evt: IngestEvent = serde_json::from_slice(&trimmed)?;
            lines.push(event_to_line(evt));
        }
    }

    if !lines.is_empty() {
        state.store.insert(lines).await?;
    }
    Ok(())
}

fn event_to_line(evt: IngestEvent) -> Line {
    let ts = evt.timestamp.unwrap_or_else(Utc::now);
    Line {
        event_id: evt.event_id,
        date: ts.format("%Y-%m-%d").to_string(),
        time: ts.format("%H:%M:%S").to_string(),
        host: evt.host,
        path: evt.path,
        query: evt.query,
        ip: evt.ip,
        user_agent: evt.user_agent,
        referrer: evt.referrer,
        r#type: content_type_to_type(&evt.content_type),
        agent: String::new(),
        os: String::new(),
        ref_domain: String::new(),
        mult: 0,
        set_cookie: evt.set_cookie,
        uniq: evt.uniq,
        second_visit: evt.second_visit,
    }
}

fn content_type_to_type(content_type: &str) -> String {
    let ct = content_type.to_lowercase();
    if ct.starts_with("application/atom+xml") || ct.starts_with("application/rss+xml") {
        "feed".to_string()
    } else {
        String::new()
    }
}
