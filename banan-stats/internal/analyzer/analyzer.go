package analyzer

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Line struct {
	Date        string
	Time        string
	Host        string
	Path        string
	Query       string
	IP          string
	UserAgent   string
	Referrer    string
	Type        string
	Agent       string
	OS          string
	RefDomain   string
	Mult        int
	SetCookie   string
	Uniq        string
	SecondVisit bool
}

func Analyze(line *Line) {
	if line.Agent == "" {
		line.Agent = lineAgent(line.UserAgent)
	}
	if line.Type == "" {
		line.Type = lineType(line.Path, line.Agent, line.UserAgent)
	}
	if line.OS == "" {
		line.OS = lineOS(line.UserAgent)
	}
	if line.Mult == 0 {
		line.Mult = lineMultiplier(line.UserAgent)
	}
	if line.Uniq == "" {
		line.Uniq = lineUniq(line.IP, line.UserAgent, line.Agent)
	}
	if line.RefDomain == "" {
		line.RefDomain = lineRefDomain(line.Referrer)
	}
}

func dequote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return s[1 : len(s)-1]
	}
	return s
}

type matcher func(string) string

var (
	reSpecial       = regexp.MustCompile(`(?i)(?:Leed|BeyondPod|360Spider|Lark|Nutch|Skype|leakix\.net|uni-app)`)
	reUUIDPrefix    = regexp.MustCompile(`(?i)^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}/\d+ ([^;(/]+)`)
	reCompatible    = regexp.MustCompile(`(?i)compatible; ([^;(/+]*[^;(/+ ])`)
	reBotBefore     = regexp.MustCompile(`(?i)^[\w\.\-_@ ]*[\w\.\-_@] (?:ro)?bot`)
	reBotContains   = regexp.MustCompile(`(?i)\b[\w-_]+bot\b`)
	reTrident       = regexp.MustCompile(`(?i)Trident/[0-9.]+`)
	reMozillaFirst  = regexp.MustCompile(`(?i)^Mozilla/.* ([A-Za-z0-9_]+)/[A-Z0-9.]+(?: (?:Chrome|Version|Mobile|Safari|Mobile Safari)/[A-Z0-9.]+)+$`)
	reMozillaSafari = regexp.MustCompile(`(?i)^Mozilla/.* ([A-Za-z0-9_]+)/[0-9.]+(?: Mobile)? Safari/[0-9.]+$`)
	reMozillaLast   = regexp.MustCompile(`(?i)^Mozilla/.* ([A-Za-z0-9_]+)/[a-z0-9.]+(?: \([^\)]+\)| Mobile| GTB[0-9.]+)*$`)
	reFeedIDName    = regexp.MustCompile(`(?i)^([\w\.\-_@ ]*[\w\.\-_@]) feed-id:`)
	reBeforeDash    = regexp.MustCompile(`(?i)^([\w\._@ ]*[\w\._@]) - `)
	reBeforeVersion = regexp.MustCompile(`(?i)^([\w\.\-_@ ]*[\w\.\-_@])[- ]v?\d+\.\d+`)
	reBeforeSlash   = regexp.MustCompile(`(?i)^([\w\.\-_@% ]*[\w\.\-_@%]) ?[/\(:\+]`)
	reSingleWord    = regexp.MustCompile(`(?i)^[\w\.\-_@ ]*[\w\.\-_@]$`)

	reRSS       = regexp.MustCompile(`(?i)rss`)
	reBotUA     = regexp.MustCompile(`(?i)bot|crawl|fetch|node|ruby|.rb|python|curl|okhttp|spider|scan|nutch|mastodon|\+http`)
	reOSAndroid = regexp.MustCompile(`(?i)Android`)
	reOSWindows = regexp.MustCompile(`(?i)Windows`)
	reOSiOS     = regexp.MustCompile(`(?i)iOS|iPhone|iPad|Mobile.*Safari`)
	reOSMac     = regexp.MustCompile(`(?i)macOS|Mac OS|Macintosh|Darwin`)
	reOSLinux   = regexp.MustCompile(`(?i)Linux|X11`)

	reMultiplier = regexp.MustCompile(`(?i)(\d+) subscriber`)
	reFeedID     = regexp.MustCompile(`(?i)feed-id[=:]([A-Za-z0-9_]+)`)
)

func regexMatch(re *regexp.Regexp) matcher {
	return func(s string) string {
		if re == nil {
			return ""
		}
		return re.FindString(s)
	}
}

func regexGroup(re *regexp.Regexp, idx int) matcher {
	return func(s string) string {
		m := re.FindStringSubmatch(s)
		if len(m) > idx {
			return m[idx]
		}
		return ""
	}
}

func lineAgent(userAgent string) string {
	if userAgent == "" {
		return ""
	}
	ua := dequote(userAgent)

	matchers := []matcher{
		regexMatch(reSpecial),
		regexGroup(reUUIDPrefix, 1),
		regexGroup(reCompatible, 1),
		regexMatch(reBotBefore),
		regexMatch(reBotContains),
		func(s string) string {
			if m := reTrident.FindString(s); m != "" {
				return "Trident"
			}
			return ""
		},
		func(s string) string {
			if m := reMozillaFirst.FindStringSubmatch(s); len(m) > 1 {
				name := m[1]
				if !isExcludedAgent(name) {
					return name
				}
			}
			return ""
		},
		func(s string) string {
			if m := reMozillaSafari.FindStringSubmatch(s); len(m) > 1 {
				name := m[1]
				if !strings.EqualFold(name, "Version") {
					return name
				}
			}
			return ""
		},
		func(s string) string {
			if m := reMozillaLast.FindStringSubmatch(s); len(m) > 1 {
				return m[1]
			}
			return ""
		},
		regexGroup(reFeedIDName, 1),
		regexGroup(reBeforeDash, 1),
		regexGroup(reBeforeVersion, 1),
		func(s string) string {
			if m := reBeforeSlash.FindStringSubmatch(s); len(m) > 1 {
				val := m[1]
				if !strings.EqualFold(val, "mozilla") && !strings.HasPrefix(strings.ToLower(val), "mozilla") {
					return val
				}
			}
			return ""
		},
		regexMatch(reSingleWord),
	}

	for _, match := range matchers {
		if val := strings.TrimSpace(match(ua)); val != "" {
			return val
		}
	}
	return ""
}

func isExcludedAgent(name string) bool {
	switch name {
	case "Chrome", "Version", "Mobile", "Safari", "Mobile Safari":
		return true
	default:
		return false
	}
}

func lineType(path, agent, userAgent string) string {
	ua := userAgent
	if ua != "" && reRSS.FindStringIndex(ua) != nil {
		return "feed"
	}
	switch agent {
	case "Chrome", "Firefox", "Edg", "EdgA", "EdgiOS", "Safari", "OPR", "YaBrowser", "Vivaldi", "SamsungBrowser", "UCBrowser":
		return "browser"
	}
	if ua != "" && reBotUA.FindStringIndex(ua) != nil {
		return "bot"
	}
	if strings.HasPrefix(ua, "Mozilla/") {
		return "browser"
	}
	return "bot"
}

func lineOS(userAgent string) string {
	ua := userAgent
	switch {
	case reOSAndroid.FindStringIndex(ua) != nil:
		return "Android"
	case reOSWindows.FindStringIndex(ua) != nil:
		return "Windows"
	case reOSiOS.FindStringIndex(ua) != nil:
		return "iOS"
	case reOSMac.FindStringIndex(ua) != nil:
		return "macOS"
	case reOSLinux.FindStringIndex(ua) != nil:
		return "Linux"
	default:
		return ""
	}
}

func lineMultiplier(userAgent string) int {
	if m := reMultiplier.FindStringSubmatch(userAgent); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 1
}

func lineUniq(ip, userAgent, agent string) string {
	if userAgent != "" && agent != "" {
		if feedID := extractFeedID(userAgent); feedID != "" {
			return hashUUID(agent + "/" + feedID)
		}
		if strings.Contains(strings.ToLower(userAgent), "subscriber") {
			return hashUUID(agent)
		}
	}
	return hashUUID(ip + userAgent)
}

func extractFeedID(userAgent string) string {
	if m := reFeedID.FindStringSubmatch(userAgent); len(m) > 1 {
		return m[1]
	}
	return ""
}

func lineRefDomain(referrer string) string {
	if referrer == "" {
		return ""
	}
	u, err := url.Parse(referrer)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func hashUUID(input string) string {
	sum := sha256.Sum256([]byte(input))
	return uuidFromBytes(sum[:16])
}

func uuidFromBytes(b []byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}
