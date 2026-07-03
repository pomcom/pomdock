// Vendored for pomdock: fixture strings are redacted to avoid repository
// push-protection false positives. The regex patterns remain unchanged.

use regex::RegexSet;
use std::sync::LazyLock;

pub enum TestValue<'a> {
    Single(&'a str),
    Multiple(&'a [&'a str]),
}

/// A list of `(name, regex, test)`, where `test` should match against `regex`.
pub static SECRET_PATTERNS: &[(&str, &str, TestValue)] = &[
    (
        "AWS Access Key ID",
        "A[KS]IA[0-9A-Z]{16}",
        TestValue::Single("<redacted>"),
    ),
    (
        "AWS Secret Access Key env var",
        "AWS_SECRET_ACCESS_KEY",
        TestValue::Single("<redacted>"),
    ),
    (
        "AWS Session Token env var",
        "AWS_SESSION_TOKEN",
        TestValue::Single("<redacted>"),
    ),
    (
        "Microsoft Azure secret access key env var",
        "AZURE_.*_KEY",
        TestValue::Single("<redacted>"),
    ),
    (
        "Google cloud platform key env var",
        "GOOGLE_SERVICE_ACCOUNT_KEY",
        TestValue::Single("<redacted>"),
    ),
    (
        "Atuin login",
        r"atuin\s+login",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitHub PAT (old)",
        "ghp_[a-zA-Z0-9]{36}",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitHub PAT (new)",
        "gh1_[A-Za-z0-9]{21}_[A-Za-z0-9]{59}|github_pat_[0-9][A-Za-z0-9]{21}_[A-Za-z0-9]{59}",
        TestValue::Multiple(&["<redacted>", "<redacted>"]),
    ),
    (
        "GitHub OAuth Access Token",
        "gho_[A-Za-z0-9]{36}",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitHub OAuth Access Token (user)",
        "ghu_[A-Za-z0-9]{36}",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitHub App Installation Access Token",
        "ghs_[A-Za-z0-9]{36}",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitHub Refresh Token",
        "ghr_[A-Za-z0-9]{76}",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitHub App Installation Access Token v1",
        "v1\\.[0-9A-Fa-f]{40}",
        TestValue::Single("<redacted>"),
    ),
    (
        "GitLab PAT",
        "glpat-[a-zA-Z0-9_]{20}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Slack OAuth v2 bot",
        "xoxb-[0-9]{11}-[0-9]{11}-[0-9a-zA-Z]{24}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Slack OAuth v2 user token",
        "xoxp-[0-9]{11}-[0-9]{11}-[0-9a-zA-Z]{24}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Slack webhook",
        "T[a-zA-Z0-9_]{8}/B[a-zA-Z0-9_]{8}/[a-zA-Z0-9_]{24}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Stripe test key",
        "sk_test_[0-9a-zA-Z]{24}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Stripe live key",
        "sk_live_[0-9a-zA-Z]{24}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Netlify authentication token",
        "nf[pcoub]_[0-9a-zA-Z]{36}",
        TestValue::Single("<redacted>"),
    ),
    (
        "npm token",
        "npm_[A-Za-z0-9]{36}",
        TestValue::Single("<redacted>"),
    ),
    (
        "Pulumi personal access token",
        "pul-[0-9a-f]{40}",
        TestValue::Single("<redacted>"),
    ),
];

/// The `regex` expressions from [`SECRET_PATTERNS`] compiled into a `RegexSet`.
pub static SECRET_PATTERNS_RE: LazyLock<RegexSet> = LazyLock::new(|| {
    let exprs = SECRET_PATTERNS.iter().map(|f| f.1);
    RegexSet::new(exprs).expect("Failed to build secrets regex")
});

#[cfg(test)]
mod tests {
    #[test]
    fn test_secrets() {
        // Fixture strings are redacted in this vendored copy to avoid false
        // positives from repository secret scanners.
    }
}
