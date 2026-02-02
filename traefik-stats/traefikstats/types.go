package traefikstats

import "time"

type event struct {
	EventID     string    `json:"eventId"`
	Timestamp   time.Time `json:"timestamp"`
	Host        string    `json:"host"`
	Path        string    `json:"path"`
	Query       string    `json:"query"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	Referrer    string    `json:"referrer"`
	ContentType string    `json:"contentType"`
	SetCookie   string    `json:"setCookie"`
	Uniq        string    `json:"uniq"`
	SecondVisit bool      `json:"secondVisit"`
}
