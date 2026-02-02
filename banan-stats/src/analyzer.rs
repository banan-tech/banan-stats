use once_cell::sync::Lazy;
use regex::Regex;
use sha2::{Digest, Sha256};
use std::borrow::Cow;
use url::Url;

#[derive(Clone, Debug)]
pub struct Line {
    pub event_id: String,
    pub date: String,
    pub time: String,
    pub host: String,
    pub path: String,
    pub query: String,
    pub ip: String,
    pub user_agent: String,
    pub referrer: String,
    pub r#type: String,
    pub agent: String,
    pub os: String,
    pub ref_domain: String,
    pub mult: i64,
    pub set_cookie: String,
    pub uniq: String,
    pub second_visit: bool,
}

pub fn analyze(line: &mut Line) {
    if line.agent.is_empty() {
        line.agent = line_agent(&line.user_agent);
    }
    if line.r#type.is_empty() {
        line.r#type = line_type(&line.path, &line.agent, &line.user_agent);
    }
    if line.os.is_empty() {
        line.os = line_os(&line.user_agent);
    }
    if line.mult == 0 {
        line.mult = line_multiplier(&line.user_agent);
    }
    if line.uniq.is_empty() {
        line.uniq = line_uniq(&line.ip, &line.user_agent, &line.agent);
    }
    if line.ref_domain.is_empty() {
        line.ref_domain = line_ref_domain(&line.referrer);
    }
}

fn dequote(s: &str) -> Cow<'_, str> {
    if s.len() >= 2 && s.starts_with('"') && s.ends_with('"') {
        return Cow::Owned(s[1..s.len() - 1].to_string());
    }
    Cow::Borrowed(s)
}

static RE_SPECIAL: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(?i)(?:Leed|BeyondPod|360Spider|Lark|Nutch|Skype|leakix\.net|uni-app)")
        .expect("re")
});
static RE_UUID_PREFIX: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(?i)^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}/\d+ ([^;(/]+)")
        .expect("re")
});
static RE_COMPATIBLE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)compatible; ([^;(/+]*[^;(/+ ])").expect("re"));
static RE_BOT_BEFORE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)^[\w\.\-_@ ]*[\w\.\-_@] (?:ro)?bot").expect("re"));
static RE_BOT_CONTAINS: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)\b[\w-_]+bot\b").expect("re"));
static RE_TRIDENT: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?i)Trident/[0-9.]+").expect("re"));
static RE_MOZILLA_FIRST: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(?i)^Mozilla/.* ([A-Za-z0-9_]+)/[A-Z0-9.]+(?: (?:Chrome|Version|Mobile|Safari|Mobile Safari)/[A-Z0-9.]+)+$")
        .expect("re")
});
static RE_MOZILLA_SAFARI: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(?i)^Mozilla/.* ([A-Za-z0-9_]+)/[0-9.]+(?: Mobile)? Safari/[0-9.]+$")
        .expect("re")
});
static RE_MOZILLA_LAST: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(?i)^Mozilla/.* ([A-Za-z0-9_]+)/[a-z0-9.]+(?: \([^\)]+\)| Mobile| GTB[0-9.]+)*$")
        .expect("re")
});
static RE_FEED_ID_NAME: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)^([\w\.\-_@ ]*[\w\.\-_@]) feed-id:").expect("re"));
static RE_BEFORE_DASH: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)^([\w\._@ ]*[\w\._@]) - ").expect("re"));
static RE_BEFORE_VERSION: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)^([\w\.\-_@ ]*[\w\.\-_@])[- ]v?\d+\.\d+").expect("re"));
static RE_BEFORE_SLASH: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)^([\w\.\-_@% ]*[\w\.\-_@%]) ?[/\(:\+]").expect("re"));
static RE_SINGLE_WORD: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)^[\w\.\-_@ ]*[\w\.\-_@]$").expect("re"));

static RE_RSS: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?i)rss").expect("re"));
static RE_BOT_UA: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(?i)bot|crawl|fetch|node|ruby|.rb|python|curl|okhttp|spider|scan|nutch|mastodon|\+http")
        .expect("re")
});
static RE_OS_ANDROID: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?i)Android").expect("re"));
static RE_OS_WINDOWS: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?i)Windows").expect("re"));
static RE_OS_IOS: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)iOS|iPhone|iPad|Mobile.*Safari").expect("re"));
static RE_OS_MAC: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?i)macOS|Mac OS|Macintosh|Darwin").expect("re"));
static RE_OS_LINUX: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?i)Linux|X11").expect("re"));

static RE_MULTIPLIER: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)(\d+) subscriber").expect("re"));
static RE_FEED_ID: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?i)feed-id[=:]([A-Za-z0-9_]+)").expect("re"));

fn line_agent(user_agent: &str) -> String {
    if user_agent.is_empty() {
        return String::new();
    }
    let ua = dequote(user_agent);

    let matchers: &[fn(&str) -> String] = &[
        |s| regex_match(&RE_SPECIAL, s),
        |s| regex_group(&RE_UUID_PREFIX, s, 1),
        |s| regex_group(&RE_COMPATIBLE, s, 1),
        |s| regex_match(&RE_BOT_BEFORE, s),
        |s| regex_match(&RE_BOT_CONTAINS, s),
        |s| {
            if RE_TRIDENT.is_match(s) {
                "Trident".to_string()
            } else {
                String::new()
            }
        },
        |s| {
            if let Some(caps) = RE_MOZILLA_FIRST.captures(s) {
                let name = caps.get(1).map(|m| m.as_str()).unwrap_or("");
                if !is_excluded_agent(name) {
                    return name.to_string();
                }
            }
            String::new()
        },
        |s| {
            if let Some(caps) = RE_MOZILLA_SAFARI.captures(s) {
                let name = caps.get(1).map(|m| m.as_str()).unwrap_or("");
                if !name.eq_ignore_ascii_case("Version") {
                    return name.to_string();
                }
            }
            String::new()
        },
        |s| {
            if let Some(caps) = RE_MOZILLA_LAST.captures(s) {
                let name = caps.get(1).map(|m| m.as_str()).unwrap_or("");
                return name.to_string();
            }
            String::new()
        },
        |s| regex_group(&RE_FEED_ID_NAME, s, 1),
        |s| regex_group(&RE_BEFORE_DASH, s, 1),
        |s| regex_group(&RE_BEFORE_VERSION, s, 1),
        |s| {
            if let Some(caps) = RE_BEFORE_SLASH.captures(s) {
                let val = caps.get(1).map(|m| m.as_str()).unwrap_or("");
                if !val.eq_ignore_ascii_case("mozilla")
                    && !val.to_lowercase().starts_with("mozilla")
                {
                    return val.to_string();
                }
            }
            String::new()
        },
        |s| regex_match(&RE_SINGLE_WORD, s),
    ];

    for matcher in matchers {
        let val = matcher(ua.as_ref()).trim().to_string();
        if !val.is_empty() {
            return val;
        }
    }
    String::new()
}

fn is_excluded_agent(name: &str) -> bool {
    matches!(
        name,
        "Chrome" | "Version" | "Mobile" | "Safari" | "Mobile Safari"
    )
}

fn line_type(path: &str, agent: &str, user_agent: &str) -> String {
    if !user_agent.is_empty() && RE_RSS.is_match(user_agent) {
        return "feed".to_string();
    }
    match agent {
        "Chrome" | "Firefox" | "Edg" | "EdgA" | "EdgiOS" | "Safari" | "OPR" | "YaBrowser"
        | "Vivaldi" | "SamsungBrowser" | "UCBrowser" => return "browser".to_string(),
        _ => {}
    }
    if !user_agent.is_empty() && RE_BOT_UA.is_match(user_agent) {
        return "bot".to_string();
    }
    if user_agent.starts_with("Mozilla/") {
        return "browser".to_string();
    }
    if path.is_empty() {
        return "bot".to_string();
    }
    "bot".to_string()
}

fn line_os(user_agent: &str) -> String {
    if RE_OS_ANDROID.is_match(user_agent) {
        return "Android".to_string();
    }
    if RE_OS_WINDOWS.is_match(user_agent) {
        return "Windows".to_string();
    }
    if RE_OS_IOS.is_match(user_agent) {
        return "iOS".to_string();
    }
    if RE_OS_MAC.is_match(user_agent) {
        return "macOS".to_string();
    }
    if RE_OS_LINUX.is_match(user_agent) {
        return "Linux".to_string();
    }
    String::new()
}

fn line_multiplier(user_agent: &str) -> i64 {
    if let Some(caps) = RE_MULTIPLIER.captures(user_agent) {
        if let Some(m) = caps.get(1) {
            if let Ok(n) = m.as_str().parse::<i64>() {
                return n;
            }
        }
    }
    1
}

fn line_uniq(ip: &str, user_agent: &str, agent: &str) -> String {
    if !user_agent.is_empty() && !agent.is_empty() {
        if let Some(feed_id) = extract_feed_id(user_agent) {
            return hash_uuid(&format!("{}/{}", agent, feed_id));
        }
        if user_agent.to_lowercase().contains("subscriber") {
            return hash_uuid(agent);
        }
    }
    hash_uuid(&format!("{}{}", ip, user_agent))
}

fn extract_feed_id(user_agent: &str) -> Option<String> {
    if let Some(caps) = RE_FEED_ID.captures(user_agent) {
        return caps.get(1).map(|m| m.as_str().to_string());
    }
    None
}

fn line_ref_domain(referrer: &str) -> String {
    if referrer.is_empty() {
        return String::new();
    }
    if let Ok(u) = Url::parse(referrer) {
        if let Some(host) = u.host_str() {
            return host.trim_start_matches("www.").to_string();
        }
    }
    String::new()
}

fn hash_uuid(input: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(input.as_bytes());
    let sum = hasher.finalize();
    uuid_from_bytes(&sum[..16])
}

fn uuid_from_bytes(b: &[u8]) -> String {
    let mut buf = [0u8; 36];
    hex::encode_to_slice(&b[0..4], &mut buf[0..8]).expect("hex");
    buf[8] = b'-';
    hex::encode_to_slice(&b[4..6], &mut buf[9..13]).expect("hex");
    buf[13] = b'-';
    hex::encode_to_slice(&b[6..8], &mut buf[14..18]).expect("hex");
    buf[18] = b'-';
    hex::encode_to_slice(&b[8..10], &mut buf[19..23]).expect("hex");
    buf[23] = b'-';
    hex::encode_to_slice(&b[10..16], &mut buf[24..36]).expect("hex");
    String::from_utf8_lossy(&buf).to_string()
}

fn regex_match(re: &Regex, s: &str) -> String {
    re.find(s).map(|m| m.as_str().to_string()).unwrap_or_default()
}

fn regex_group(re: &Regex, s: &str, idx: usize) -> String {
    re.captures(s)
        .and_then(|caps| caps.get(idx).map(|m| m.as_str().to_string()))
        .unwrap_or_default()
}
