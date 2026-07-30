use chrono::{DateTime, NaiveDate, Utc};
use clap::{Parser, ValueEnum};
use indicatif::{ProgressBar, ProgressStyle};
use rayon::prelude::*;
use reqwest::blocking::Client;
use reqwest::StatusCode;
use serde::Deserialize;
use std::collections::{HashSet, VecDeque};
use std::fs::{self, File};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Duration;
use url::Url;

static TEMP_FILE_COUNTER: AtomicUsize = AtomicUsize::new(0);

#[derive(Parser, Debug)]
#[command(
    author,
    version,
    about = "Download URLs from sitemaps and save HTML responses."
)]
struct Cli {
    /// Path to the text file containing URLs or sitemap URLs (one per line)
    #[arg(short, long)]
    input: PathBuf,

    /// How to interpret the input file (auto, sitemap, url)
    #[arg(long, default_value = "auto")]
    input_type: InputType,

    /// Directory to write downloaded HTML files
    #[arg(short, long, default_value = "processed_urls")]
    output: PathBuf,

    /// Skip URLs older than this many days (based on sitemap lastmod). URLs without lastmod are included.
    #[arg(long)]
    max_age_days: Option<u32>,

    /// Allow non-US hosts (default: only .us, .gov, .mil, .edu)
    #[arg(long, default_value_t = false)]
    allow_all_countries: bool,

    /// Try fetching a .md variant before .html
    #[arg(long, default_value_t = false)]
    try_markdown: bool,

    /// Skip downloading if the output file already exists on disk
    #[arg(long, default_value_t = false)]
    skip_existing: bool,

    /// Delay in milliseconds between HTTP requests (rate limiting)
    #[arg(long)]
    delay_ms: Option<u64>,

    /// Continue and exit successfully even if one or more URL downloads fail
    #[arg(long, default_value_t = false)]
    allow_failures: bool,

    /// Total request timeout in seconds
    #[arg(long, default_value_t = 60)]
    request_timeout_secs: u64,

    /// TCP connect timeout in seconds
    #[arg(long, default_value_t = 10)]
    connect_timeout_secs: u64,

    /// Number of retries after the initial HTTP request attempt
    #[arg(long, default_value_t = 2)]
    retries: u32,

    /// Initial retry backoff in milliseconds; each retry doubles this value
    #[arg(long, default_value_t = 500)]
    retry_backoff_ms: u64,
}

#[derive(Copy, Clone, Debug, ValueEnum)]
enum InputType {
    Auto,
    Sitemap,
    Url,
}

#[derive(Copy, Clone, Debug)]
struct RetryConfig {
    retries: u32,
    backoff_ms: u64,
}

fn main() -> Result<(), String> {
    if let Err(err) = run() {
        eprintln!("{err}");
        std::process::exit(1);
    }

    Ok(())
}

fn run() -> Result<(), String> {
    let cli = Cli::parse();

    if !cli.input.exists() {
        return Err(format!("Input file {:?} does not exist", cli.input));
    }

    if !cli.output.exists() {
        fs::create_dir_all(&cli.output)
            .map_err(|err| format!("Failed to create output directory: {err}"))?;
    }

    let retry_config = RetryConfig {
        retries: cli.retries,
        backoff_ms: cli.retry_backoff_ms,
    };

    let client = Client::builder()
        .user_agent("nv-ingest/26.1.2")
        .timeout(Duration::from_secs(cli.request_timeout_secs))
        .connect_timeout(Duration::from_secs(cli.connect_timeout_secs))
        .build()
        .map_err(|err| format!("Failed to build HTTP client: {err}"))?;

    let file = File::open(&cli.input)
        .map_err(|err| format!("Failed to open input file {:?}: {err}", cli.input))?;
    let reader = BufReader::new(file);

    let input_entries: Vec<(usize, String)> = reader
        .lines()
        .enumerate()
        .filter_map(|(index, line_result)| match line_result {
            Ok(content) => Some((index + 1, content.trim().to_string())),
            Err(err) => {
                eprintln!("Skipping line {}: failed to read ({err})", index + 1);
                None
            }
        })
        .filter(|(_, line)| !line.is_empty() && !line.starts_with('#'))
        .collect();

    if input_entries.is_empty() {
        return Err(
            "No inputs were processed. Ensure the input file has valid, non-comment lines."
                .to_string(),
        );
    }

    let input_urls = resolve_input_urls(
        &client,
        cli.input_type,
        &input_entries,
        cli.max_age_days,
        cli.allow_all_countries,
        retry_config,
    )?;

    if input_urls.is_empty() {
        return Err("No URLs were found in the provided inputs.".to_string());
    }

    let total = input_urls.len() as u64;
    let pb = ProgressBar::new(total);
    pb.set_style(
        ProgressStyle::default_bar()
            .template("[{elapsed_precise}] {bar:40.cyan/blue} {pos}/{len} {msg}")
            .unwrap()
            .progress_chars("#>-"),
    );
    pb.set_message("Downloading URLs...");

    let processed_count = Arc::new(AtomicUsize::new(0));
    let failed_count = Arc::new(AtomicUsize::new(0));
    let skipped_count = Arc::new(AtomicUsize::new(0));

    let results: Vec<Result<PathBuf, String>> = input_urls
        .par_iter()
        .map(|(line_number, line)| {
            let url = Url::parse(line)
                .map_err(|err| format!("Skipping line {line_number}: invalid URL ({err})"))?;

            // Check if output file already exists before downloading
            if cli.skip_existing {
                if let Some(existing) = existing_path(&url, &cli.output, cli.try_markdown) {
                    let count = skipped_count.fetch_add(1, Ordering::Relaxed) + 1;
                    pb.inc(1);
                    pb.set_message(format!(
                        "Processed: {} | Skipped: {} | Failed: {}",
                        processed_count.load(Ordering::Relaxed),
                        count,
                        failed_count.load(Ordering::Relaxed),
                    ));
                    return Ok(existing);
                }
            }

            let result =
                download_and_save(&client, &url, &cli.output, cli.try_markdown, retry_config);

            if let Some(ms) = cli.delay_ms {
                std::thread::sleep(std::time::Duration::from_millis(ms));
            }

            // Update progress bar and counters
            pb.inc(1);
            match &result {
                Ok(_) => {
                    let count = processed_count.fetch_add(1, Ordering::Relaxed) + 1;
                    pb.set_message(format!(
                        "Processed: {} | Skipped: {} | Failed: {}",
                        count,
                        skipped_count.load(Ordering::Relaxed),
                        failed_count.load(Ordering::Relaxed),
                    ));
                }
                Err(_) => {
                    let count = failed_count.fetch_add(1, Ordering::Relaxed) + 1;
                    pb.set_message(format!(
                        "Processed: {} | Skipped: {} | Failed: {}",
                        processed_count.load(Ordering::Relaxed),
                        skipped_count.load(Ordering::Relaxed),
                        count,
                    ));
                }
            }

            result
        })
        .collect();

    pb.finish_with_message(format!(
        "Completed! Processed: {} | Skipped: {} | Failed: {}",
        processed_count.load(Ordering::Relaxed),
        skipped_count.load(Ordering::Relaxed),
        failed_count.load(Ordering::Relaxed)
    ));

    // Print summary of errors if any
    if failed_count.load(Ordering::Relaxed) > 0 {
        eprintln!("\n\nErrors encountered:");
        for (entry, result) in input_urls.iter().zip(results.iter()) {
            if let Err(err) = result {
                eprintln!("  {} - {}", entry.1, err);
            }
        }
    }

    println!("\nTotal URLs: {}", input_urls.len());
    println!(
        "Successfully downloaded: {}",
        processed_count.load(Ordering::Relaxed)
    );
    println!(
        "Skipped (already exist): {}",
        skipped_count.load(Ordering::Relaxed)
    );
    println!("Failed: {}", failed_count.load(Ordering::Relaxed));

    ensure_no_unaccepted_failures(failed_count.load(Ordering::Relaxed), cli.allow_failures)?;

    Ok(())
}

fn ensure_no_unaccepted_failures(failed_count: usize, allow_failures: bool) -> Result<(), String> {
    if failed_count > 0 && !allow_failures {
        return Err(format!(
            "{failed_count} download(s) failed; rerun with --allow-failures to accept partial crawl output"
        ));
    }
    Ok(())
}

/// Check if a downloaded file already exists for this URL, returning its path if so.
fn existing_path(url: &Url, output_dir: &Path, try_markdown: bool) -> Option<PathBuf> {
    if try_markdown {
        // Check markdown variant first (preferred format)
        if let Some(md_url) = markdown_variant(url) {
            if let Ok(md_path) = url_to_path_with_ext(&md_url, output_dir, "md") {
                if md_path.exists() {
                    return Some(md_path);
                }
            }
        }
        if let Ok(md_path) = url_to_path_with_ext(url, output_dir, "md") {
            if md_path.exists() {
                return Some(md_path);
            }
        }
    }
    if let Ok(html_path) = url_to_path_with_ext(url, output_dir, "html") {
        if html_path.exists() {
            return Some(html_path);
        }
    }
    None
}

fn download_and_save(
    client: &Client,
    url: &Url,
    output_dir: &Path,
    try_markdown: bool,
    retry_config: RetryConfig,
) -> Result<PathBuf, String> {
    if try_markdown {
        if is_markdown_path(url) {
            return download_with_ext(client, url, output_dir, "md", retry_config);
        }

        if let Some(md_url) = markdown_variant(url) {
            if let Ok(path) = download_with_ext(client, &md_url, output_dir, "md", retry_config) {
                return Ok(path);
            }
        }
    }

    download_with_ext(client, url, output_dir, "html", retry_config)
}

fn download_with_ext(
    client: &Client,
    url: &Url,
    output_dir: &Path,
    ext: &str,
    retry_config: RetryConfig,
) -> Result<PathBuf, String> {
    let bytes = fetch_bytes(client, url, retry_config)?;
    let path = url_to_path_with_ext(url, output_dir, ext)?;
    write_atomic(&path, &bytes)?;

    Ok(path)
}

fn fetch_bytes(client: &Client, url: &Url, retry_config: RetryConfig) -> Result<Vec<u8>, String> {
    retry_http(retry_config, || {
        let response = client
            .get(url.clone())
            .send()
            .map_err(|err| format!("request error: {err}"))?;

        if response.status() != StatusCode::OK {
            return Err(format!("non-OK status: {}", response.status()));
        }

        response
            .bytes()
            .map(|bytes| bytes.to_vec())
            .map_err(|err| format!("failed to read body: {err}"))
    })
}

fn fetch_text(client: &Client, url: &Url, retry_config: RetryConfig) -> Result<String, String> {
    retry_http(retry_config, || {
        let response = client
            .get(url.clone())
            .send()
            .map_err(|err| format!("request error: {err}"))?;

        if response.status() != StatusCode::OK {
            return Err(format!("non-OK status: {}", response.status()));
        }

        response
            .text()
            .map_err(|err| format!("failed to read body: {err}"))
    })
}

fn retry_http<T, F>(retry_config: RetryConfig, mut operation: F) -> Result<T, String>
where
    F: FnMut() -> Result<T, String>,
{
    let mut last_error = None;
    for attempt in 0..=retry_config.retries {
        match operation() {
            Ok(value) => return Ok(value),
            Err(err) => {
                last_error = Some(err);
                if attempt < retry_config.retries {
                    let multiplier = 1_u64 << attempt.min(16);
                    std::thread::sleep(Duration::from_millis(
                        retry_config.backoff_ms.saturating_mul(multiplier),
                    ));
                }
            }
        }
    }

    Err(last_error.unwrap_or_else(|| "request failed".to_string()))
}

fn write_atomic(path: &Path, bytes: &[u8]) -> Result<(), String> {
    let tmp_path = temporary_path(path);
    let write_result = (|| -> Result<(), String> {
        let mut file = File::create(&tmp_path)
            .map_err(|err| format!("failed to create temporary file {:?}: {err}", tmp_path))?;
        file.write_all(bytes)
            .map_err(|err| format!("failed to write temporary file {:?}: {err}", tmp_path))?;
        file.sync_all()
            .map_err(|err| format!("failed to sync temporary file {:?}: {err}", tmp_path))?;
        fs::rename(&tmp_path, path)
            .map_err(|err| format!("failed to rename {:?} to {:?}: {err}", tmp_path, path))?;
        Ok(())
    })();

    if write_result.is_err() {
        let _ = fs::remove_file(&tmp_path);
    }

    write_result
}

fn temporary_path(path: &Path) -> PathBuf {
    let counter = TEMP_FILE_COUNTER.fetch_add(1, Ordering::Relaxed);
    let extension = path
        .extension()
        .and_then(|value| value.to_str())
        .unwrap_or("download");
    path.with_extension(format!("{extension}.tmp-{}-{counter}", process::id()))
}

fn url_to_path_with_ext(url: &Url, output_dir: &Path, ext: &str) -> Result<PathBuf, String> {
    let host = url.host_str().unwrap_or("unknown");

    // Sanitize the path portion of the URL
    let url_path = url.path().trim_matches('/');
    let sanitized_path = if url_path.is_empty() {
        "index".to_string()
    } else {
        url_path
            .chars()
            .map(|c| match c {
                '/' => '_',
                '\\' | ':' | '*' | '?' | '"' | '<' | '>' | '|' => '_',
                _ => c,
            })
            .collect()
    };

    // Include query string if present (sanitized)
    let query_suffix = url
        .query()
        .map(|q| {
            let sanitized: String = q
                .chars()
                .map(|c| match c {
                    '&' | '=' | '?' | '#' | '/' | '\\' | ':' | '*' | '"' | '<' | '>' | '|' => '_',
                    _ => c,
                })
                .collect();
            format!("_{}", sanitized)
        })
        .unwrap_or_default();

    // Create host subdirectory
    let mut host_dir = output_dir.to_path_buf();
    host_dir.push(host);
    fs::create_dir_all(&host_dir)
        .map_err(|err| format!("failed to create host directory {:?}: {err}", host_dir))?;

    // Build final path
    let filename = format!("{}{}.{}", sanitized_path, query_suffix, ext);
    let mut full_path = host_dir;
    full_path.push(filename);

    Ok(full_path)
}

#[derive(Debug, Deserialize)]
#[serde(rename = "sitemapindex")]
struct SitemapIndex {
    #[serde(rename = "sitemap")]
    sitemaps: Vec<SitemapEntry>,
}

#[derive(Debug, Deserialize)]
struct SitemapEntry {
    #[serde(rename = "loc")]
    loc: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename = "urlset")]
struct UrlSet {
    #[serde(rename = "url")]
    urls: Vec<UrlEntry>,
}

#[derive(Debug, Deserialize)]
struct UrlEntry {
    #[serde(rename = "loc")]
    loc: String,
    #[serde(rename = "lastmod")]
    lastmod: Option<String>,
}

/// Represents a parsed URL with optional last modification date
#[derive(Debug, Clone)]
struct ParsedUrl {
    loc: String,
    lastmod: Option<DateTime<Utc>>,
}

enum SitemapParse {
    Index(Vec<String>),
    UrlSet(Vec<ParsedUrl>),
}

fn resolve_input_urls(
    client: &Client,
    input_type: InputType,
    input_entries: &[(usize, String)],
    max_age_days: Option<u32>,
    allow_all_countries: bool,
    retry_config: RetryConfig,
) -> Result<Vec<(usize, String)>, String> {
    let mut direct_urls: Vec<String> = Vec::new();
    let mut sitemap_entries: Vec<(usize, String)> = Vec::new();

    match input_type {
        InputType::Sitemap => {
            sitemap_entries.extend_from_slice(input_entries);
        }
        InputType::Url => {
            direct_urls.extend(input_entries.iter().map(|(_, line)| line.clone()));
        }
        InputType::Auto => {
            for (line_number, line) in input_entries {
                if looks_like_sitemap_url(line) {
                    sitemap_entries.push((*line_number, line.clone()));
                } else {
                    direct_urls.push(line.clone());
                }
            }
        }
    }

    let mut merged_urls: Vec<String> = Vec::new();
    let mut seen: HashSet<String> = HashSet::new();

    for url in direct_urls {
        if seen.insert(url.clone()) {
            merged_urls.push(url);
        }
    }

    if !sitemap_entries.is_empty() {
        let sitemap_urls =
            collect_sitemap_urls(client, &sitemap_entries, max_age_days, retry_config);
        for url in sitemap_urls {
            if seen.insert(url.clone()) {
                merged_urls.push(url);
            }
        }
    }

    if !allow_all_countries {
        merged_urls.retain(|url| match Url::parse(url) {
            Ok(parsed) => is_us_host(&parsed),
            Err(_) => true,
        });
    }

    Ok(merged_urls
        .into_iter()
        .enumerate()
        .map(|(index, url)| (index + 1, url))
        .collect())
}

fn collect_sitemap_urls(
    client: &Client,
    sitemap_entries: &[(usize, String)],
    max_age_days: Option<u32>,
    retry_config: RetryConfig,
) -> Vec<String> {
    let mut queue = VecDeque::new();
    for (line_number, line) in sitemap_entries {
        match Url::parse(line) {
            Ok(sitemap_url) => queue.push_back(sitemap_url),
            Err(err) => {
                eprintln!("Skipping line {line_number}: invalid sitemap URL ({err})");
            }
        }
    }

    let mut visited_sitemaps: HashSet<String> = HashSet::new();
    let mut discovered_urls: HashSet<String> = HashSet::new();
    let mut skipped_count: usize = 0;

    // Calculate cutoff date if max_age_days is specified
    let cutoff_date = max_age_days.map(|days| Utc::now() - chrono::Duration::days(days as i64));

    while let Some(sitemap_url) = queue.pop_front() {
        let sitemap_key = sitemap_url.as_str().to_string();
        if !visited_sitemaps.insert(sitemap_key.clone()) {
            continue;
        }

        let xml = match download_sitemap(client, &sitemap_url, retry_config) {
            Ok(content) => content,
            Err(err) => {
                eprintln!("Failed to read sitemap {sitemap_key}: {err}");
                continue;
            }
        };

        let parsed = match parse_sitemap(&xml) {
            Ok(result) => result,
            Err(err) => {
                eprintln!("Failed to parse sitemap {sitemap_key}: {err}");
                continue;
            }
        };

        match parsed {
            SitemapParse::Index(sitemaps) => {
                for entry in sitemaps {
                    match Url::parse(&entry) {
                        Ok(url) => queue.push_back(url),
                        Err(err) => {
                            eprintln!("Skipping sitemap entry {entry}: invalid URL ({err})")
                        }
                    }
                }
            }
            SitemapParse::UrlSet(urls) => {
                for parsed_url in urls {
                    // Skip only when lastmod exists and is older than cutoff.
                    // If lastmod is missing or unparsable, keep the URL.
                    if let Some(cutoff) = cutoff_date {
                        if let Some(lastmod) = parsed_url.lastmod {
                            if lastmod < cutoff {
                                skipped_count += 1;
                                continue;
                            }
                        }
                    }

                    match Url::parse(&parsed_url.loc) {
                        Ok(url) => {
                            discovered_urls.insert(url.as_str().to_string());
                        }
                        Err(err) => {
                            eprintln!("Skipping url entry {}: invalid URL ({err})", parsed_url.loc)
                        }
                    }
                }
            }
        }
    }

    if skipped_count > 0 {
        eprintln!(
            "Skipped {skipped_count} URLs due to age (older than {} days)",
            max_age_days.unwrap_or(0)
        );
    }

    let mut urls: Vec<String> = discovered_urls.into_iter().collect();
    urls.sort();

    urls
}

fn download_sitemap(
    client: &Client,
    url: &Url,
    retry_config: RetryConfig,
) -> Result<String, String> {
    fetch_text(client, url, retry_config)
}

fn parse_sitemap(xml: &str) -> Result<SitemapParse, String> {
    if let Ok(index) = quick_xml::de::from_str::<SitemapIndex>(xml) {
        let sitemaps = index.sitemaps.into_iter().map(|entry| entry.loc).collect();
        return Ok(SitemapParse::Index(sitemaps));
    }

    if let Ok(urlset) = quick_xml::de::from_str::<UrlSet>(xml) {
        let urls = urlset
            .urls
            .into_iter()
            .map(|entry| ParsedUrl {
                loc: entry.loc,
                lastmod: entry.lastmod.and_then(|s| parse_lastmod(&s)),
            })
            .collect();
        return Ok(SitemapParse::UrlSet(urls));
    }

    Err("unrecognized sitemap format".to_string())
}

/// Parse a sitemap lastmod date string into a DateTime<Utc>.
/// Supports various formats: full ISO 8601, date-only, etc.
fn parse_lastmod(s: &str) -> Option<DateTime<Utc>> {
    // Try parsing as full ISO 8601 datetime (e.g., "2024-01-15T10:30:00+00:00")
    if let Ok(dt) = DateTime::parse_from_rfc3339(s) {
        return Some(dt.with_timezone(&Utc));
    }

    // Try parsing as ISO 8601 without timezone (e.g., "2024-01-15T10:30:00")
    if let Ok(dt) = chrono::NaiveDateTime::parse_from_str(s, "%Y-%m-%dT%H:%M:%S") {
        return Some(dt.and_utc());
    }

    // Try parsing as date only (e.g., "2024-01-15")
    if let Ok(date) = NaiveDate::parse_from_str(s, "%Y-%m-%d") {
        return Some(date.and_hms_opt(0, 0, 0)?.and_utc());
    }

    None
}

fn looks_like_sitemap_url(line: &str) -> bool {
    let lower = line.to_ascii_lowercase();
    lower.contains("sitemap") || lower.ends_with(".xml")
}

fn is_us_host(url: &Url) -> bool {
    let host = match url.host_str() {
        Some(value) => value.to_ascii_lowercase(),
        None => return false,
    };

    host.ends_with(".us")
        || host.ends_with(".gov")
        || host.ends_with(".mil")
        || host.ends_with(".edu")
        || host.ends_with(".com")
        || host.ends_with(".net")
        || host.ends_with(".org")
        || host.ends_with(".io")
        || host.ends_with(".ai")
        || host.ends_with(".dev")
        || host.ends_with(".tech")
        || host.ends_with(".space")
        || host.ends_with(".info")
}

fn is_markdown_path(url: &Url) -> bool {
    url.path().to_ascii_lowercase().ends_with(".md")
}

fn markdown_variant(url: &Url) -> Option<Url> {
    if is_markdown_path(url) {
        return Some(url.clone());
    }

    let path = url.path().trim_end_matches('/');
    let md_path = if path.is_empty() {
        "/index.md".to_string()
    } else {
        format!("{path}.md")
    };

    let mut md_url = url.clone();
    md_url.set_path(&md_path);
    Some(md_url)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn unique_test_dir(name: &str) -> PathBuf {
        let counter = TEMP_FILE_COUNTER.fetch_add(1, Ordering::Relaxed);
        let path = std::env::temp_dir().join(format!(
            "url_ingest_test_{}_{}_{}",
            name,
            process::id(),
            counter
        ));
        let _ = fs::remove_dir_all(&path);
        fs::create_dir_all(&path).unwrap();
        path
    }

    #[test]
    fn failed_downloads_fail_closed_by_default() {
        assert!(ensure_no_unaccepted_failures(1, false).is_err());
        assert!(ensure_no_unaccepted_failures(1, true).is_ok());
        assert!(ensure_no_unaccepted_failures(0, false).is_ok());
    }

    #[test]
    fn atomic_write_replaces_target_and_leaves_no_temp_file() {
        let dir = unique_test_dir("atomic");
        let target = dir.join("example.html");

        write_atomic(&target, b"first").unwrap();
        assert_eq!(fs::read(&target).unwrap(), b"first");

        write_atomic(&target, b"second").unwrap();
        assert_eq!(fs::read(&target).unwrap(), b"second");

        let temp_files: Vec<_> = fs::read_dir(&dir)
            .unwrap()
            .filter_map(Result::ok)
            .filter(|entry| entry.file_name().to_string_lossy().contains(".tmp-"))
            .collect();
        assert!(temp_files.is_empty());

        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn url_to_path_sanitizes_path_and_query() {
        let dir = unique_test_dir("path");
        let url = Url::parse("https://example.com/a/b:c?x=1&y=/two").unwrap();
        let path = url_to_path_with_ext(&url, &dir, "html").unwrap();
        assert_eq!(
            path.file_name().unwrap().to_string_lossy(),
            "a_b_c_x_1_y__two.html"
        );
        assert_eq!(
            path.parent()
                .unwrap()
                .file_name()
                .unwrap()
                .to_string_lossy(),
            "example.com"
        );
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn retry_http_retries_until_success() {
        let mut attempts = 0;
        let result = retry_http(
            RetryConfig {
                retries: 2,
                backoff_ms: 0,
            },
            || {
                attempts += 1;
                if attempts < 3 {
                    Err("temporary".to_string())
                } else {
                    Ok("ok")
                }
            },
        );

        assert_eq!(result.unwrap(), "ok");
        assert_eq!(attempts, 3);
    }

    #[test]
    fn markdown_variant_appends_md() {
        let url = Url::parse("https://docs.example.com/path/page").unwrap();
        let md_url = markdown_variant(&url).unwrap();
        assert_eq!(md_url.as_str(), "https://docs.example.com/path/page.md");
    }
}
