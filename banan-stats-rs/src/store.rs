use crate::analyzer::{self, Line};
use anyhow::Context;
use duckdb::{params, Connection};
use std::sync::{Arc, Mutex};

pub struct Store {
    conn: Arc<Mutex<Connection>>,
}

impl Store {
    pub fn open(path: &str) -> Result<Self, anyhow::Error> {
        let conn = Connection::open(path).with_context(|| format!("open db {}", path))?;
        for stmt in [
            "CREATE TYPE agent_type_t AS ENUM ('feed', 'bot', 'browser')",
            "CREATE TYPE agent_os_t AS ENUM ('Android', 'Windows', 'iOS', 'macOS', 'Linux')",
        ] {
            if let Err(err) = conn.execute(stmt, []) {
                if !is_existing_type_error(&err) {
                    return Err(err.into());
                }
            }
        }

        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS stats (
                 date       DATE,
                 time       TIME,
                 host       VARCHAR,
                 path       VARCHAR,
                 query      VARCHAR,
                 ip         VARCHAR,
                 user_agent VARCHAR,
                 referrer   VARCHAR,
                 type       agent_type_t,
                 agent      VARCHAR,
                 os         agent_os_t,
                 ref_domain VARCHAR,
                 mult       INTEGER,
                 set_cookie UUID,
                 uniq       UUID
             );
             ALTER TABLE stats ADD COLUMN IF NOT EXISTS host VARCHAR;
             CREATE INDEX IF NOT EXISTS idx_stats_host_date ON stats(host, date);",
        )?;

        Ok(Self {
            conn: Arc::new(Mutex::new(conn)),
        })
    }

    pub async fn insert(&self, lines: Vec<Line>) -> Result<(), anyhow::Error> {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || -> Result<(), anyhow::Error> {
            let mut conn = conn.lock().expect("db lock");
            let tx = conn.transaction()?;

            let mut stmt = tx.prepare(
                "INSERT INTO stats
                 (date, time, host, path, query, ip, user_agent, referrer, type, agent, os, ref_domain, mult, set_cookie, uniq)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            )?;
            let mut upd_stmt = tx.prepare("UPDATE stats SET uniq = ? WHERE set_cookie = ?")?;

            for mut line in lines {
                analyzer::analyze(&mut line);
                stmt.execute(params![
                    null_str(&line.date),
                    null_str(&line.time),
                    null_str(&line.host),
                    null_str(&line.path),
                    null_str(&line.query),
                    null_str(&line.ip),
                    null_str(&line.user_agent),
                    null_str(&line.referrer),
                    null_str(&line.r#type),
                    null_str(&line.agent),
                    null_str(&line.os),
                    null_str(&line.ref_domain),
                    line.mult,
                    null_str(&line.set_cookie),
                    null_str(&line.uniq),
                ])?;

                if line.second_visit && !line.uniq.is_empty() {
                    upd_stmt.execute(params![line.uniq, line.uniq])?;
                }
            }

            tx.commit()?;
            Ok(())
        })
        .await??;
        Ok(())
    }

    pub async fn with_conn<T, F>(&self, func: F) -> Result<T, anyhow::Error>
    where
        T: Send + 'static,
        F: FnOnce(&Connection) -> Result<T, anyhow::Error> + Send + 'static,
    {
        let conn = self.conn.clone();
        tokio::task::spawn_blocking(move || {
            let conn = conn.lock().expect("db lock");
            func(&conn)
        })
        .await?
    }
}

fn null_str(s: &str) -> Option<&str> {
    if s.is_empty() {
        None
    } else {
        Some(s)
    }
}

fn is_existing_type_error(err: &duckdb::Error) -> bool {
    let msg = err.to_string();
    msg.contains("already exists") || msg.contains("Type with name")
}
