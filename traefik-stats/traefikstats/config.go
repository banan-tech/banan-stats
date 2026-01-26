package traefikstats

import "time"

type Config struct {
	SidecarURL     string `json:"sidecarURL" yaml:"sidecarURL" toml:"sidecarURL"`
	DashboardPath  string `json:"dashboardPath" yaml:"dashboardPath" toml:"dashboardPath"`
	DashboardToken string `json:"dashboardToken" yaml:"dashboardToken" toml:"dashboardToken"`

	CookieName     string `json:"cookieName" yaml:"cookieName" toml:"cookieName"`
	CookiePath     string `json:"cookiePath" yaml:"cookiePath" toml:"cookiePath"`
	CookieDomain   string `json:"cookieDomain" yaml:"cookieDomain" toml:"cookieDomain"`
	CookieMaxAge   int    `json:"cookieMaxAge" yaml:"cookieMaxAge" toml:"cookieMaxAge"`
	CookieSecure   bool   `json:"cookieSecure" yaml:"cookieSecure" toml:"cookieSecure"`
	CookieHTTPOnly bool   `json:"cookieHTTPOnly" yaml:"cookieHTTPOnly" toml:"cookieHTTPOnly"`
	CookieSameSite string `json:"cookieSameSite" yaml:"cookieSameSite" toml:"cookieSameSite"`

	QueueSize      int    `json:"queueSize" yaml:"queueSize" toml:"queueSize"`
	FlushInterval  string `json:"flushInterval" yaml:"flushInterval" toml:"flushInterval"`
	HostFilterMode string `json:"hostFilterMode" yaml:"hostFilterMode" toml:"hostFilterMode"`
}

func CreateConfig() *Config {
	return &Config{
		SidecarURL:     "",
		DashboardPath:  "/stats",
		DashboardToken: "",

		CookieName:     "stats_id",
		CookiePath:     "/",
		CookieDomain:   "",
		CookieMaxAge:   2147483647,
		CookieSecure:   false,
		CookieHTTPOnly: true,
		CookieSameSite: "Lax",

		QueueSize:      1024,
		FlushInterval:  (2 * time.Second).String(),
		HostFilterMode: "per-host",
	}
}
