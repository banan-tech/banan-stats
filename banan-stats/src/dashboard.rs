use crate::state::AppState;
use crate::store::Store;
use axum::{
    extract::{RawQuery, State},
    http::HeaderMap,
    response::{IntoResponse, Redirect, Response},
    routing::get,
    Router,
};
use chrono::{Datelike, Duration, NaiveDate, Utc};
use duckdb::params_from_iter;
use std::collections::HashMap;
use std::fmt::Write;

const STYLE_CSS: &str = include_str!("../assets/style.css");
const SCRIPT_JS: &str = include_str!("../assets/script.js");

const YEAR_MONTH_FORMAT: &str = "%Y-%m";

const ALLOWED_FILTERS: &[&str] = &["host", "path", "query", "ref_domain", "agent", "type", "os"];

pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/stats", get(stats_handler))
        .route("/stats/favicon.ico", get(favicon_handler))
        .with_state(state)
}

async fn favicon_handler() -> impl IntoResponse {
    axum::http::StatusCode::NO_CONTENT
}

async fn stats_handler(
    State(state): State<AppState>,
    RawQuery(raw): RawQuery,
) -> Response {
    let params = parse_query(raw.unwrap_or_default());
    let from_str = first_value(&params, "from");
    let to_str = first_value(&params, "to");

    let (from_str, to_str) = match (from_str, to_str) {
        (Some(from), Some(to)) => (from, to),
        _ => return redirect_to_year("/stats", &params).into_response(),
    };

    let from_date = match NaiveDate::parse_from_str(&from_str, "%Y-%m-%d") {
        Ok(val) => val,
        Err(_) => return redirect_to_year("/stats", &params).into_response(),
    };
    let to_date = match NaiveDate::parse_from_str(&to_str, "%Y-%m-%d") {
        Ok(val) => val,
        Err(_) => return redirect_to_year("/stats", &params).into_response(),
    };

    let filters = extract_filters(&params);
    let (where_clause, args) = build_where(&from_str, &to_str, &filters);

    let (min_date, max_date) = match min_max_date(&state.store).await {
        Ok(val) => val,
        Err(_) => default_year_range(),
    };
    let hosts = distinct_hosts(&state.store).await.unwrap_or_default();

    let visits = visits_by_type_date(&state.store, &where_clause, &args)
        .await
        .unwrap_or_default();
    let totals = total_uniq(&state.store, &where_clause, &args)
        .await
        .unwrap_or_default();

    let mut body = String::new();
    append(&mut body, "<!DOCTYPE html>");
    append(&mut body, "<html>");
    append(&mut body, "<head>");
    append(&mut body, "<meta charset=\"utf-8\">");
    append(
        &mut body,
        &format!(
            "<link rel='icon' href='/stats/favicon.ico' sizes='32x32'>"
        ),
    );
    append(
        &mut body,
        "<link rel=\"preconnect\" href=\"https://fonts.gstatic.com\" crossorigin>",
    );
    append(
        &mut body,
        "<link href=\"https://fonts.googleapis.com/css2?family=Inter:opsz,wght@14..32,100..900&display=swap\" rel=\"stylesheet\">",
    );
    append(&mut body, &format!("<style>{}</style>", STYLE_CSS));
    append(&mut body, &format!("<script>{}</script>", SCRIPT_JS));
    append(&mut body, "</head>");
    append(&mut body, "<body>");

    append(&mut body, "<div class=filters>");
    append_year_filters(
        &mut body,
        &params,
        from_date,
        to_date,
        min_date,
        max_date,
    );
    append_host_filters(&mut body, &params, &hosts);
    append_active_filters(&mut body, &params);
    append(&mut body, "</div>");

    append_timelines(
        &mut body,
        &visits,
        &totals,
        &params,
        from_date,
        to_date,
    );
    append_tables(&mut body, &state.store, &where_clause, &args, &params).await;

    append(&mut body, "</body>");
    append(&mut body, "</html>");

    let mut headers = HeaderMap::new();
    headers.insert(
        "Content-Type",
        "text/html; charset=utf-8".parse().expect("header"),
    );
    (headers, body).into_response()
}

fn append(out: &mut String, value: &str) {
    let _ = writeln!(out, "{}", value);
}

fn parse_query(raw: String) -> HashMap<String, Vec<String>> {
    let mut params: HashMap<String, Vec<String>> = HashMap::new();
    for (k, v) in url::form_urlencoded::parse(raw.as_bytes()) {
        params
            .entry(k.to_string())
            .or_default()
            .push(v.to_string());
    }
    params
}

fn first_value(params: &HashMap<String, Vec<String>>, key: &str) -> Option<String> {
    params.get(key).and_then(|vals| vals.get(0)).cloned()
}

fn redirect_to_year(path: &str, params: &HashMap<String, Vec<String>>) -> Redirect {
    let now = Utc::now().date_naive();
    let from = NaiveDate::from_ymd_opt(now.year(), 1, 1).unwrap();
    let to = NaiveDate::from_ymd_opt(now.year(), 12, 31).unwrap();
    let mut new_params = clone_params(params);
    new_params.insert("from".to_string(), vec![from.format("%Y-%m-%d").to_string()]);
    new_params.insert("to".to_string(), vec![to.format("%Y-%m-%d").to_string()]);
    let query = encode_params(&new_params);
    Redirect::to(&format!("{}?{}", path, query))
}

fn extract_filters(params: &HashMap<String, Vec<String>>) -> HashMap<String, String> {
    let mut filters = HashMap::new();
    for (key, values) in params {
        if key == "from" || key == "to" {
            continue;
        }
        if !ALLOWED_FILTERS.contains(&key.as_str()) || values.is_empty() {
            continue;
        }
        filters.insert(key.clone(), values[0].clone());
    }
    filters
}

fn build_where(from_str: &str, to_str: &str, filters: &HashMap<String, String>) -> (String, Vec<String>) {
    let mut where_parts = vec!["date >= ?".to_string(), "date <= ?".to_string()];
    let mut args = vec![from_str.to_string(), to_str.to_string()];
    for (key, val) in filters {
        where_parts.push(format!("{} = ?", key));
        args.push(val.clone());
    }
    (where_parts.join(" AND "), args)
}

async fn min_max_date(store: &Store) -> Result<(NaiveDate, NaiveDate), anyhow::Error> {
    store
        .with_conn(|conn| {
            let mut stmt = conn.prepare("SELECT min(date), max(date) FROM stats")?;
            let mut rows = stmt.query([])?;
            let now = Utc::now().date_naive();
            let mut min = NaiveDate::from_ymd_opt(now.year(), 1, 1).unwrap();
            let mut max = NaiveDate::from_ymd_opt(now.year(), 12, 31).unwrap();
            if let Some(row) = rows.next()? {
                let min_date: Option<NaiveDate> = row.get(0)?;
                let max_date: Option<NaiveDate> = row.get(1)?;
                if let Some(val) = min_date {
                    min = val;
                }
                if let Some(val) = max_date {
                    max = val;
                }
            }
            Ok((min, max))
        })
        .await
}

fn default_year_range() -> (NaiveDate, NaiveDate) {
    let now = Utc::now().date_naive();
    (
        NaiveDate::from_ymd_opt(now.year(), 1, 1).unwrap(),
        NaiveDate::from_ymd_opt(now.year(), 12, 31).unwrap(),
    )
}

async fn distinct_hosts(store: &Store) -> Result<Vec<String>, anyhow::Error> {
    store
        .with_conn(|conn| {
            let mut stmt = conn.prepare(
                "SELECT DISTINCT host FROM stats WHERE host IS NOT NULL ORDER BY host",
            )?;
            let mut rows = stmt.query([])?;
            let mut hosts = Vec::new();
            while let Some(row) = rows.next()? {
                let host: Option<String> = row.get(0)?;
                if let Some(host) = host {
                    if !host.is_empty() {
                        hosts.push(host);
                    }
                }
            }
            Ok(hosts)
        })
        .await
}

async fn visits_by_type_date(
    store: &Store,
    where_clause: &str,
    args: &[String],
) -> Result<HashMap<String, HashMap<NaiveDate, i64>>, anyhow::Error> {
    let query = format!(
        "WITH subq AS (
            SELECT type, date, MAX(mult) AS mult
            FROM stats
            WHERE {}
            GROUP BY type, date, uniq
        )
        SELECT type, date, SUM(mult) AS cnt
        FROM subq
        GROUP BY type, date",
        where_clause
    );
    let args = args.to_owned();
    store
        .with_conn(move |conn| {
            let mut stmt = conn.prepare(&query)?;
            let params = params_from_iter(args.iter().map(|s| s.as_str()));
            let mut rows = stmt.query(params)?;
            let mut result: HashMap<String, HashMap<NaiveDate, i64>> = HashMap::new();
            while let Some(row) = rows.next()? {
                let typ: Option<String> = row.get(0)?;
                let date: NaiveDate = row.get(1)?;
                let cnt: i64 = row.get(2)?;
                if let Some(typ) = typ {
                    result.entry(typ).or_default().insert(date, cnt);
                }
            }
            Ok(result)
        })
        .await
}

async fn total_uniq(
    store: &Store,
    where_clause: &str,
    args: &[String],
) -> Result<HashMap<String, i64>, anyhow::Error> {
    let query = format!(
        "WITH subq AS (
            SELECT type, MAX(mult) AS mult
            FROM stats
            WHERE {}
            GROUP BY type, uniq
        )
        SELECT type, SUM(mult) AS cnt
        FROM subq
        GROUP BY type",
        where_clause
    );
    let args = args.to_owned();
    store
        .with_conn(move |conn| {
            let mut stmt = conn.prepare(&query)?;
            let params = params_from_iter(args.iter().map(|s| s.as_str()));
            let mut rows = stmt.query(params)?;
            let mut result: HashMap<String, i64> = HashMap::new();
            while let Some(row) = rows.next()? {
                let typ: Option<String> = row.get(0)?;
                let cnt: i64 = row.get(1)?;
                if let Some(typ) = typ {
                    result.insert(typ, cnt);
                }
            }
            Ok(result)
        })
        .await
}

fn append_year_filters(
    out: &mut String,
    params: &HashMap<String, Vec<String>>,
    from_date: NaiveDate,
    to_date: NaiveDate,
    min_date: NaiveDate,
    max_date: NaiveDate,
) {
    let min_year = min_date.year();
    let max_year = max_date.year();

    let mut all_params = clone_params(params);
    all_params.insert(
        "from".to_string(),
        vec![min_date.format("%Y-%m-%d").to_string()],
    );
    all_params.insert(
        "to".to_string(),
        vec![max_date.format("%Y-%m-%d").to_string()],
    );
    append(
        out,
        &format!("<a class=filter href='?{}'>All</a>", encode_params(&all_params)),
    );

    for year in min_year..=max_year {
        let mut qs = clone_params(params);
        qs.insert("from".to_string(), vec![format!("{}-01-01", year)]);
        qs.insert("to".to_string(), vec![format!("{}-12-31", year)]);
        let mut link = format!("<a href='?{}' class='filter", encode_params(&qs));
        if from_date.year() <= year && to_date.year() >= year {
            link.push_str(" in");
        }
        link.push_str(&format!("'>{}</a>", year));
        append(out, &link);
    }
}

fn append_host_filters(out: &mut String, params: &HashMap<String, Vec<String>>, hosts: &[String]) {
    for host in hosts {
        let mut qs = clone_params(params);
        qs.insert("host".to_string(), vec![host.to_string()]);
        append(
            out,
            &format!(
                "<a href='?{}' class='filter'>{}</a>",
                encode_params(&qs),
                host
            ),
        );
    }
}

fn append_active_filters(out: &mut String, params: &HashMap<String, Vec<String>>) {
    for (key, values) in params {
        if key == "from" || key == "to" || values.is_empty() {
            continue;
        }
        let mut qs = clone_params(params);
        qs.remove(key);
        append(
            out,
            &format!(
                "<div class=filter>{}: {}<a href='?{}'>&times;</a></div>",
                key,
                values[0],
                encode_params(&qs)
            ),
        );
    }
}

fn append_timelines(
    out: &mut String,
    data: &HashMap<String, HashMap<NaiveDate, i64>>,
    totals: &HashMap<String, i64>,
    params: &HashMap<String, Vec<String>>,
    from_date: NaiveDate,
    to_date: NaiveDate,
) {
    let mut max_val = 1i64;
    for date_counts in data.values() {
        for val in date_counts.values() {
            if *val > max_val {
                max_val = *val;
            }
        }
    }
    max_val = round_max_val(max_val);

    let dates = list_dates(from_date, to_date);
    let graph_w = dates.len() * 3;

    let bar_height = |v: i64| -> i64 { (v * 100) / max_val.max(1) };
    let hrz_step = horizontal_step(max_val);

    let sections = [
        ("browser", "Unique visitors"),
        ("feed", "RSS Readers"),
        ("bot", "Scrapers"),
    ];

    for (typ, title) in sections {
        let Some(date_counts) = data.get(typ) else { continue };
        if date_counts.is_empty() {
            continue;
        }
        if typ == "feed" {
            append(
                out,
                &format!(
                    "<h1>{}: ~{} / day</h1>",
                    title,
                    format_number_with_commas(average(date_counts))
                ),
            );
        } else {
            append(
                out,
                &format!(
                    "<h1>{}: {}</h1>",
                    title,
                    format_number_with_commas(*totals.get(typ).unwrap_or(&0))
                ),
            );
        }
        append(out, "<div class=graph_outer>");
        append(out, "<div class=graph_scroll>");
        append(
            out,
            &format!("<svg class=graph width={} height=130>", graph_w),
        );

        let mut val = 0;
        while val <= max_val {
            let bar_h = bar_height(val);
            append(
                out,
                &format!(
                    "<line class=hrz x1=0 y1={} x2={} y2={} />",
                    110 - bar_h,
                    graph_w,
                    110 - bar_h
                ),
            );
            val += hrz_step;
        }

        for (idx, date) in dates.iter().enumerate() {
            let val = *date_counts.get(date).unwrap_or(&0);
            if val > 0 {
                let bar_h = bar_height(val);
                let data_v = format_num(val);
                let data_d = date.format("%Y-%m-%d");
                let x = idx * 3;
                let y = 110 - bar_h as usize;
                append(
                    out,
                    &format!(
                        "<g data-v='{}' data-d='{}'><rect class=i x={} y=0 width=3 height=110 />\
                         <rect x={} y={} width=3 height={} /><line x1={} y1={} x2={} y2={} /></g>",
                        data_v,
                        data_d,
                        x,
                        x,
                        y.saturating_sub(2),
                        bar_h + 2,
                        x,
                        y.saturating_sub(1),
                        x + 3,
                        y.saturating_sub(1)
                    ),
                );
            }
            if date.day() == 1 {
                let month_end = (*date + Duration::days(32))
                    .with_day(1)
                    .unwrap()
                    - Duration::days(1);
                let mut qs = clone_params(params);
                qs.insert("from".to_string(), vec![date.format("%Y-%m-%d").to_string()]);
                qs.insert(
                    "to".to_string(),
                    vec![month_end.format("%Y-%m-%d").to_string()],
                );
                append(
                    out,
                    &format!(
                        "<line class=date x1={} y1=112 x2={} y2=120 />\
                         <a href='?{}'><text x={} y=130>{}</text></a>",
                        idx * 3,
                        idx * 3,
                        encode_params(&qs),
                        idx * 3,
                        date.format(YEAR_MONTH_FORMAT)
                    ),
                );
            }
            if same_day(*date, Utc::now().date_naive()) {
                append(
                    out,
                    &format!(
                        "<line class=today x1={} y1=0 x2={} y2=120 />",
                        (idx * 3) + 1,
                        (idx * 3) + 1
                    ),
                );
            }
        }
        append(out, "</svg>");
        append(out, "</div>");

        append(out, "<svg class=graph_legend height=130>");
        let mut val = 0;
        while val <= max_val {
            let bar_h = bar_height(val);
            append(
                out,
                &format!(
                    "<text x=20 y={} text-anchor=end>{}</text>",
                    113 - bar_h,
                    format_num(val)
                ),
            );
            val += hrz_step;
        }
        append(out, "</svg>");

        append(out, "<div class=graph_hover style='display: none'></div>");
        append(out, "</div>");
    }
}

async fn append_tables(
    out: &mut String,
    store: &Store,
    where_clause: &str,
    args: &[String],
    params: &HashMap<String, Vec<String>>,
) {
    append(out, "<div class=tables>");
    append_table(
        out,
        store,
        "Paths",
        "path",
        &format!("{} AND type = 'browser'", where_clause),
        args,
        params,
        "path",
        Some(|v: String| v),
    )
    .await;
    append_table(
        out,
        store,
        "Queries",
        "query",
        &format!("{} AND type = 'browser'", where_clause),
        args,
        params,
        "query",
        None,
    )
    .await;
    append_table(
        out,
        store,
        "Referrers",
        "ref_domain",
        &format!("{} AND type = 'browser'", where_clause),
        args,
        params,
        "ref_domain",
        Some(|v| format!("https://{}", v)),
    )
    .await;
    append_table_uniq(
        out,
        store,
        "Browsers",
        "agent",
        &format!("{} AND type = 'browser'", where_clause),
        args,
        params,
        "agent",
    )
    .await;
    append_table_uniq(
        out,
        store,
        "RSS Readers",
        "agent",
        &format!("{} AND type = 'feed'", where_clause),
        args,
        params,
        "agent",
    )
    .await;
    append_table_uniq(
        out,
        store,
        "Scrapers",
        "agent",
        &format!("{} AND type = 'bot'", where_clause),
        args,
        params,
        "agent",
    )
    .await;
    append(out, "</div>");
}

#[derive(Clone)]
struct RowCount {
    value: String,
    count: i64,
}

async fn append_table(
    out: &mut String,
    store: &Store,
    title: &str,
    column: &str,
    where_clause: &str,
    args: &[String],
    params: &HashMap<String, Vec<String>>,
    filter_param: &str,
    href_fn: Option<fn(String) -> String>,
) {
    let rows = top10(store, column, where_clause, args).await.unwrap_or_default();
    if rows.is_empty() {
        return;
    }
    append(out, "<div class=table_outer>");
    append(out, &format!("<h1>{}</h1>", title));
    append(out, "<table>");
    let mut total = 0i64;
    for row in &rows {
        total += row.count;
    }
    if total == 0 {
        total = 1;
    }
    for row in rows {
        if row.count <= 0 {
            continue;
        }
        let mut percent = (row.count as f64) * 100.0 / (total as f64);
        let mut percent_str = format!("{:.0}%", percent);
        if percent < 2.0 {
            percent = (percent * 10.0).round() / 10.0;
            percent_str = format!("{:.1}%", percent);
        }
        append(out, "<tr>");
        append(out, "<td class=f>");
        if !row.value.is_empty() && !filter_param.is_empty() {
            let mut qs = clone_params(params);
            qs.insert(filter_param.to_string(), vec![row.value.clone()]);
            append(
                out,
                &format!(
                    "<a href='?{}' title='Filter by {} = {}'>&#x1F50D;</a>",
                    encode_params(&qs),
                    filter_param,
                    row.value
                ),
            );
        }
        append(out, "</td>");
        append(out, "<th>");
        append(
            out,
            &format!(
                "<div style='width: {}'{}></div>",
                percent_str,
                if row.value.is_empty() { " class=other" } else { "" }
            ),
        );
        if let Some(ref href_fn) = href_fn {
            if !row.value.is_empty() {
                append(
                    out,
                    &format!(
                        "<a href='{}' title='{}' target=_blank>{}</a>",
                        href_fn(row.value.clone()),
                        row.value,
                        row.value
                    ),
                );
            }
        }
        if href_fn.is_none() || row.value.is_empty() {
            let label = if row.value.is_empty() {
                "Others".to_string()
            } else {
                row.value.clone()
            };
            append(out, &format!("<span title='{}'>{}</span>", label, label));
        }
        append(out, &format!("<td>{}</td>", format_num(row.count)));
        append(out, &format!("<td class='pct'>{}</td>", percent_str));
        append(out, "</tr>");
    }
    append(out, "</table>");
    append(out, "</div>");
}

async fn append_table_uniq(
    out: &mut String,
    store: &Store,
    title: &str,
    column: &str,
    where_clause: &str,
    args: &[String],
    params: &HashMap<String, Vec<String>>,
    filter_param: &str,
) {
    let rows = top10_uniq(store, column, where_clause, args)
        .await
        .unwrap_or_default();
    if rows.is_empty() {
        return;
    }
    append(out, "<div class=table_outer>");
    append(out, &format!("<h1>{}</h1>", title));
    append(out, "<table>");
    let mut total = 0i64;
    for row in &rows {
        total += row.count;
    }
    if total == 0 {
        total = 1;
    }
    for row in rows {
        if row.count <= 0 {
            continue;
        }
        let mut percent = (row.count as f64) * 100.0 / (total as f64);
        let mut percent_str = format!("{:.0}%", percent);
        if percent < 2.0 {
            percent = (percent * 10.0).round() / 10.0;
            percent_str = format!("{:.1}%", percent);
        }
        append(out, "<tr>");
        append(out, "<td class=f>");
        if !row.value.is_empty() && !filter_param.is_empty() {
            let mut qs = clone_params(params);
            qs.insert(filter_param.to_string(), vec![row.value.clone()]);
            append(
                out,
                &format!(
                    "<a href='?{}' title='Filter by {} = {}'>&#x1F50D;</a>",
                    encode_params(&qs),
                    filter_param,
                    row.value
                ),
            );
        }
        append(out, "</td>");
        append(out, "<th>");
        append(
            out,
            &format!(
                "<div style='width: {}'{}></div>",
                percent_str,
                if row.value.is_empty() { " class=other" } else { "" }
            ),
        );
        let label = if row.value.is_empty() {
            "Others".to_string()
        } else {
            row.value.clone()
        };
        append(out, &format!("<span title='{}'>{}</span>", label, label));
        append(out, "</th>");
        append(out, &format!("<td>{}</td>", format_num(row.count)));
        append(out, &format!("<td class='pct'>{}</td>", percent_str));
        append(out, "</tr>");
    }
    append(out, "</table>");
    append(out, "</div>");
}

async fn top10(
    store: &Store,
    column: &str,
    where_clause: &str,
    args: &[String],
) -> Result<Vec<RowCount>, anyhow::Error> {
    let query = format!(
        "WITH base_query AS (
            SELECT {col}
            FROM stats
            WHERE {where_clause}
        ),
        top_values AS (
            SELECT {col} AS value, COUNT(*) AS count
            FROM base_query
            WHERE {col} IS NOT NULL
            GROUP BY value
            ORDER BY count DESC
        ),
        top_n AS (
            SELECT * FROM top_values ORDER BY count DESC LIMIT 10
        ),
        others AS (
            SELECT NULL AS value, COUNT(*) AS count
            FROM base_query
            WHERE {col} IS NOT NULL AND {col} NOT IN (SELECT value FROM top_n)
        )
        SELECT * FROM top_n
        UNION ALL
        SELECT * FROM others
        WHERE count > 0",
        col = column,
        where_clause = where_clause
    );
    let args = args.to_owned();
    store
        .with_conn(move |conn| {
            let mut stmt = conn.prepare(&query)?;
            let params = params_from_iter(args.iter().map(|s| s.as_str()));
            let mut rows = stmt.query(params)?;
            read_rows(&mut rows)
        })
        .await
}

async fn top10_uniq(
    store: &Store,
    column: &str,
    where_clause: &str,
    args: &[String],
) -> Result<Vec<RowCount>, anyhow::Error> {
    let query = format!(
        "WITH base_query AS (
            SELECT ANY_VALUE({col}) AS {col}, MAX(mult) AS mult
            FROM stats
            WHERE {where_clause}
            GROUP BY uniq
        ),
        top_values AS (
            SELECT {col} AS value, SUM(mult) AS count
            FROM base_query
            WHERE {col} IS NOT NULL
            GROUP BY value
            ORDER BY count DESC
        ),
        top_n AS (
            SELECT * FROM top_values ORDER BY count DESC LIMIT 10
        ),
        others AS (
            SELECT NULL AS value, SUM(mult) AS count
            FROM base_query
            WHERE {col} IS NOT NULL AND {col} NOT IN (SELECT value FROM top_n)
        )
        SELECT * FROM top_n
        UNION ALL
        SELECT * FROM others
        WHERE count > 0",
        col = column,
        where_clause = where_clause
    );
    let args = args.to_owned();
    store
        .with_conn(move |conn| {
            let mut stmt = conn.prepare(&query)?;
            let params = params_from_iter(args.iter().map(|s| s.as_str()));
            let mut rows = stmt.query(params)?;
            read_rows(&mut rows)
        })
        .await
}

fn read_rows(rows: &mut duckdb::Rows<'_>) -> Result<Vec<RowCount>, anyhow::Error> {
    let mut out = Vec::new();
    while let Some(row) = rows.next()? {
        let value: Option<String> = row.get(0)?;
        let count: i64 = row.get(1)?;
        out.push(RowCount {
            value: value.unwrap_or_default(),
            count,
        });
    }
    Ok(out)
}

fn list_dates(from_date: NaiveDate, to_date: NaiveDate) -> Vec<NaiveDate> {
    let mut dates = Vec::new();
    let mut d = from_date;
    while d <= to_date {
        dates.push(d);
        d += Duration::days(1);
    }
    dates
}

fn round_max_val(max_val: i64) -> i64 {
    match max_val {
        v if v >= 200_000 => round_to(v, 100_000),
        v if v >= 20_000 => round_to(v, 10_000),
        v if v >= 2_000 => round_to(v, 1_000),
        v if v >= 100 => round_to(v, 100),
        _ => 100,
    }
}

fn round_to(n: i64, m: i64) -> i64 {
    ((n - 1) / m + 1) * m
}

fn horizontal_step(max_val: i64) -> i64 {
    match max_val {
        v if v >= 600_000 => 200_000,
        v if v >= 300_000 => 100_000,
        v if v >= 100_000 => 50_000,
        v if v >= 60_000 => 20_000,
        v if v >= 30_000 => 10_000,
        v if v >= 10_000 => 5_000,
        v if v >= 6_000 => 2_000,
        v if v >= 3_000 => 1_000,
        v if v >= 1_000 => 500,
        v if v >= 600 => 200,
        v if v >= 300 => 100,
        v if v >= 100 => 50,
        v if v >= 60 => 20,
        _ => 10,
    }
}

fn format_num(n: i64) -> String {
    if n >= 10_000_000 {
        return trim_trailing_zero(format!("{:.0}M", n as f64 / 1_000_000.0));
    }
    if n >= 1_000_000 {
        return trim_trailing_zero(format!("{:.1}M", n as f64 / 1_000_000.0));
    }
    if n >= 10_000 {
        return trim_trailing_zero(format!("{:.0}K", n as f64 / 1_000.0));
    }
    if n >= 1_000 {
        return trim_trailing_zero(format!("{:.1}K", n as f64 / 1_000.0));
    }
    n.to_string()
}

fn trim_trailing_zero(mut s: String) -> String {
    if s.ends_with(".0M") {
        s = s.replace(".0M", "M");
    }
    if s.ends_with(".0K") {
        s = s.replace(".0K", "K");
    }
    s
}

fn format_number_with_commas(n: i64) -> String {
    let s = n.to_string();
    if s.len() <= 3 {
        return s;
    }
    let mut result = String::new();
    for (i, c) in s.chars().enumerate() {
        if i > 0 && (s.len() - i) % 3 == 0 {
            result.push(',');
        }
        result.push(c);
    }
    result
}

fn average(values: &HashMap<NaiveDate, i64>) -> i64 {
    if values.is_empty() {
        return 0;
    }
    let sum: i64 = values.values().sum();
    ((sum as f64) / (values.len() as f64) + 0.5) as i64
}

fn same_day(a: NaiveDate, b: NaiveDate) -> bool {
    a == b
}

fn clone_params(params: &HashMap<String, Vec<String>>) -> HashMap<String, Vec<String>> {
    params
        .iter()
        .map(|(k, v)| (k.clone(), v.clone()))
        .collect()
}

fn encode_params(params: &HashMap<String, Vec<String>>) -> String {
    let mut serializer = url::form_urlencoded::Serializer::new(String::new());
    let mut keys: Vec<_> = params.keys().collect();
    keys.sort();
    for key in keys {
        if let Some(values) = params.get(key) {
            for value in values {
                serializer.append_pair(key, value);
            }
        }
    }
    serializer.finish()
}
