package dashboard

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed assets/style.css
var styleCSS string

//go:embed assets/script.js
var scriptJS string

var yearMonthFormatter = "2006-01"

var allowedFilters = map[string]bool{
	"host":       true,
	"path":       true,
	"query":      true,
	"ref_domain": true,
	"agent":      true,
	"type":       true,
	"os":         true,
}

func Render(ctx context.Context, db *sql.DB, w http.ResponseWriter, req *http.Request) {
	params := req.URL.Query()
	fromStr := params.Get("from")
	toStr := params.Get("to")
	if fromStr == "" || toStr == "" {
		redirectToYear(w, req)
		return
	}

	fromDate, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		redirectToYear(w, req)
		return
	}
	toDate, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		redirectToYear(w, req)
		return
	}

	filters := extractFilters(params)
	whereClause, args := buildWhere(fromStr, toStr, filters)

	minDate, maxDate := minMaxDate(ctx, db)
	hosts := distinctHosts(ctx, db)

	visits := visitsByTypeDate(ctx, db, whereClause, args)
	totals := totalUniq(ctx, db, whereClause, args)

	builder := strings.Builder{}
	append := func(parts ...any) {
		for _, part := range parts {
			builder.WriteString(fmt.Sprint(part))
		}
		builder.WriteString("\n")
	}

	append("<!DOCTYPE html>")
	append("<html>")
	append("<head>")
	append("<meta charset=\"utf-8\">")
	append("<link rel='icon' href='", req.URL.Path, "/favicon.ico' sizes='32x32'>")
	append("<link rel=\"preconnect\" href=\"https://fonts.gstatic.com\" crossorigin>")
	append("<link href=\"https://fonts.googleapis.com/css2?family=Inter:opsz,wght@14..32,100..900&display=swap\" rel=\"stylesheet\">")
	append("<style>", styleCSS, "</style>")
	append("<script>", scriptJS, "</script>")
	append("</head>")
	append("<body>")

	append("<div class=filters>")
	appendYearFilters(append, params, fromDate, toDate, minDate, maxDate)
	appendHostFilters(append, params, hosts)
	appendActiveFilters(append, params)
	append("</div>")

	appendTimelines(append, visits, totals, params, fromDate, toDate)
	appendTables(ctx, append, db, whereClause, args, params)

	append("</body>")
	append("</html>")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(builder.String()))
}

func redirectToYear(w http.ResponseWriter, req *http.Request) {
	now := time.Now()
	from := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	params := req.URL.Query()
	params.Set("from", from.Format("2006-01-02"))
	params.Set("to", to.Format("2006-01-02"))
	u := *req.URL
	u.RawQuery = params.Encode()
	http.Redirect(w, req, u.String(), http.StatusFound)
}

func extractFilters(params url.Values) map[string]string {
	filters := map[string]string{}
	for key, values := range params {
		if key == "from" || key == "to" {
			continue
		}
		if !allowedFilters[key] || len(values) == 0 {
			continue
		}
		filters[key] = values[0]
	}
	return filters
}

func buildWhere(fromStr, toStr string, filters map[string]string) (string, []any) {
	where := []string{"date >= ?", "date <= ?"}
	args := []any{fromStr, toStr}
	for key, val := range filters {
		where = append(where, key+" = ?")
		args = append(args, val)
	}
	return strings.Join(where, " AND "), args
}

func minMaxDate(ctx context.Context, db *sql.DB) (time.Time, time.Time) {
	row := db.QueryRowContext(ctx, "SELECT min(date), max(date) FROM stats")
	var minDate, maxDate sql.NullTime
	_ = row.Scan(&minDate, &maxDate)
	now := time.Now().UTC()
	min := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	max := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	if minDate.Valid {
		min = minDate.Time
	}
	if maxDate.Valid {
		max = maxDate.Time
	}
	return min, max
}

func distinctHosts(ctx context.Context, db *sql.DB) []string {
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT host FROM stats WHERE host IS NOT NULL ORDER BY host")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var hosts []string
	for rows.Next() {
		var host sql.NullString
		if err := rows.Scan(&host); err == nil && host.Valid && host.String != "" {
			hosts = append(hosts, host.String)
		}
	}
	return hosts
}

func visitsByTypeDate(ctx context.Context, db *sql.DB, where string, args []any) map[string]map[time.Time]int64 {
	query := fmt.Sprintf(`WITH subq AS (
		SELECT type, date, MAX(mult) AS mult
		FROM stats
		WHERE %s
		GROUP BY type, date, uniq
	)
	SELECT type, date, SUM(mult) AS cnt
	FROM subq
	GROUP BY type, date`, where)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return map[string]map[time.Time]int64{}
	}
	defer rows.Close()

	result := map[string]map[time.Time]int64{}
	for rows.Next() {
		var typ sql.NullString
		var date time.Time
		var cnt int64
		if err := rows.Scan(&typ, &date, &cnt); err != nil || !typ.Valid {
			continue
		}
		if _, ok := result[typ.String]; !ok {
			result[typ.String] = map[time.Time]int64{}
		}
		result[typ.String][date] = cnt
	}
	return result
}

func totalUniq(ctx context.Context, db *sql.DB, where string, args []any) map[string]int64 {
	query := fmt.Sprintf(`WITH subq AS (
		SELECT type, MAX(mult) AS mult
		FROM stats
		WHERE %s
		GROUP BY type, uniq
	)
	SELECT type, SUM(mult) AS cnt
	FROM subq
	GROUP BY type`, where)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return map[string]int64{}
	}
	defer rows.Close()

	result := map[string]int64{}
	for rows.Next() {
		var typ sql.NullString
		var cnt int64
		if err := rows.Scan(&typ, &cnt); err != nil || !typ.Valid {
			continue
		}
		result[typ.String] = cnt
	}
	return result
}

func appendYearFilters(append func(...any), params url.Values, fromDate, toDate, minDate, maxDate time.Time) {
	minYear := minDate.Year()
	maxYear := maxDate.Year()
	allParams := cloneParams(params)
	allParams.Set("from", minDate.Format("2006-01-02"))
	allParams.Set("to", maxDate.Format("2006-01-02"))
	append("<a class=filter href='?", allParams.Encode(), "'>All</a>")

	for year := minYear; year <= maxYear; year++ {
		qs := cloneParams(params)
		qs.Set("from", fmt.Sprintf("%d-01-01", year))
		qs.Set("to", fmt.Sprintf("%d-12-31", year))
		append("<a href='?", qs.Encode(), "' class='filter")
		if fromDate.Year() <= year && toDate.Year() >= year {
			append(" in")
		}
		append("'>", year, "</a>")
	}
}

func appendHostFilters(append func(...any), params url.Values, hosts []string) {
	if len(hosts) == 0 {
		return
	}
	for _, host := range hosts {
		qs := cloneParams(params)
		qs.Set("host", host)
		append("<a href='?", qs.Encode(), "' class='filter'>", host, "</a>")
	}
}

func appendActiveFilters(append func(...any), params url.Values) {
	for key, values := range params {
		if key == "from" || key == "to" || len(values) == 0 {
			continue
		}
		append("<div class=filter>", key, ": ", values[0])
		qs := cloneParams(params)
		qs.Del(key)
		append("<a href='?", qs.Encode(), "'>×</a>")
		append("</div>")
	}
}

func appendTimelines(append func(...any), data map[string]map[time.Time]int64, totals map[string]int64, params url.Values, fromDate, toDate time.Time) {
	maxVal := int64(1)
	for _, dateCounts := range data {
		for _, val := range dateCounts {
			if val > maxVal {
				maxVal = val
			}
		}
	}
	maxVal = roundMaxVal(maxVal)

	dates := listDates(fromDate, toDate)
	graphW := len(dates) * 3

	barHeight := func(v int64) int {
		return int(float64(v) * 100 / float64(maxVal))
	}
	hrzStep := horizontalStep(maxVal)

	for _, section := range []struct {
		typ   string
		title string
	}{
		{typ: "browser", title: "Unique visitors"},
		{typ: "feed", title: "RSS Readers"},
		{typ: "bot", title: "Scrapers"},
	} {
		dateCounts := data[section.typ]
		if len(dateCounts) == 0 {
			continue
		}
		if section.typ == "feed" {
			append(fmt.Sprintf("<h1>%s: ~%s / day</h1>", section.title, formatNumberWithCommas(average(dateCounts))))
		} else {
			append(fmt.Sprintf("<h1>%s: %s</h1>", section.title, formatNumberWithCommas(totals[section.typ])))
		}
		append("<div class=graph_outer>")
		append("<div class=graph_scroll>")
		append("<svg class=graph width=", graphW, " height=", 130, ">")

		for val := int64(0); val <= maxVal; val += hrzStep {
			barH := barHeight(val)
			append("<line class=hrz x1=0 y1=", 110-barH, " x2=", graphW, " y2=", 110-barH, " />")
		}

		for idx, date := range dates {
			val := dateCounts[date]
			if val > 0 {
				barH := barHeight(val)
				dataV := formatNum(val)
				dataD := date.Format("2006-01-02")
				x := idx * 3
				y := 110 - barH
				append("<g data-v='", dataV, "' data-d='", dataD, "'>")
				append("<rect class=i x=", x, " y=0 width=3 height=110 />")
				append("<rect x=", x, " y=", y-2, " width=3 height=", barH+2, " />")
				append("<line x1=", x, " y1=", y-1, " x2=", x+3, " y2=", y-1, " />")
				append("</g>")
			}
			if date.Day() == 1 {
				monthEnd := time.Date(date.Year(), date.Month()+1, 0, 0, 0, 0, 0, time.UTC)
				qs := cloneParams(params)
				qs.Set("from", date.Format("2006-01-02"))
				qs.Set("to", monthEnd.Format("2006-01-02"))
				append("<line class=date x1=", idx*3, " y1=", 112, " x2=", idx*3, " y2=", 120, " />")
				append("<a href='?", qs.Encode(), "'>")
				append("<text x=", idx*3, " y=", 130, ">", date.Format(yearMonthFormatter), "</text>")
				append("</a>")
			}
			if sameDay(date, time.Now().UTC()) {
				append("<line class=today x1=", (idx*3)+1, " y1=0 x2=", (idx*3)+1, " y2=120 />")
			}
		}
		append("</svg>")
		append("</div>")

		append("<svg class=graph_legend height=", 130, ">")
		for val := int64(0); val <= maxVal; val += hrzStep {
			barH := barHeight(val)
			append("<text x=20 y=", 113-barH, " text-anchor=end>", formatNum(val), "</text>")
		}
		append("</svg>")

		append("<div class=graph_hover style='display: none'></div>")
		append("</div>")
	}
}

func appendTables(ctx context.Context, append func(...any), db *sql.DB, where string, args []any, params url.Values) {
	append("<div class=tables>")
	appendTable(ctx, append, db, "Paths", "path", where+" AND type = 'browser'", args, params, "path", func(v string) string { return v })
	appendTable(ctx, append, db, "Queries", "query", where+" AND type = 'browser'", args, params, "query", nil)
	appendTable(ctx, append, db, "Referrers", "ref_domain", where+" AND type = 'browser'", args, params, "ref_domain", func(v string) string { return "https://" + v })
	appendTableUniq(ctx, append, db, "Browsers", "agent", where+" AND type = 'browser'", args, params, "agent")
	appendTableUniq(ctx, append, db, "RSS Readers", "agent", where+" AND type = 'feed'", args, params, "agent")
	appendTableUniq(ctx, append, db, "Scrapers", "agent", where+" AND type = 'bot'", args, params, "agent")
	append("</div>")
}

func appendTable(ctx context.Context, append func(...any), db *sql.DB, title, column, where string, args []any, params url.Values, filterParam string, hrefFn func(string) string) {
	rows := top10(ctx, db, column, where, args)
	if len(rows) == 0 {
		return
	}
	append("<div class=table_outer>")
	append("<h1>", title, "</h1>")
	append("<table>")
	total := int64(0)
	for _, row := range rows {
		total += row.count
	}
	if total == 0 {
		total = 1
	}
	for _, row := range rows {
		if row.count <= 0 {
			continue
		}
		percent := float64(row.count) * 100.0 / float64(total)
		percentStr := fmt.Sprintf("%.0f%%", percent)
		if percent < 2.0 {
			percentStr = fmt.Sprintf("%.1f%%", percent)
		}
		append("<tr>")
		append("<td class=f>")
		if row.value != "" && filterParam != "" {
			qs := cloneParams(params)
			qs.Set(filterParam, row.value)
			append("<a href='?", qs.Encode(), "' title='Filter by ", filterParam, " = ", row.value, "'>🔍</a>")
		}
		append("</td>")
		append("<th>")
		append("<div style='width: ", percentStr, "'", func() string {
			if row.value == "" {
				return " class=other"
			}
			return ""
		}(), "'></div>")
		if hrefFn != nil && row.value != "" {
			append("<a href='", hrefFn(row.value), "' title='", row.value, "' target=_blank>", row.value, "</a>")
		} else {
			label := row.value
			if label == "" {
				label = "Others"
			}
			append("<span title='", label, "'>", label, "</span>")
		}
		append("</th>")
		append("<td>", formatNum(row.count), "</td>")
		append("<td class='pct'>", percentStr, "</td>")
		append("</tr>")
	}
	append("</table>")
	append("</div>")
}

func appendTableUniq(ctx context.Context, append func(...any), db *sql.DB, title, column, where string, args []any, params url.Values, filterParam string) {
	rows := top10Uniq(ctx, db, column, where, args)
	if len(rows) == 0 {
		return
	}
	append("<div class=table_outer>")
	append("<h1>", title, "</h1>")
	append("<table>")
	total := int64(0)
	for _, row := range rows {
		total += row.count
	}
	if total == 0 {
		total = 1
	}
	for _, row := range rows {
		if row.count <= 0 {
			continue
		}
		percent := float64(row.count) * 100.0 / float64(total)
		percentStr := fmt.Sprintf("%.0f%%", percent)
		if percent < 2.0 {
			percentStr = fmt.Sprintf("%.1f%%", percent)
		}
		append("<tr>")
		append("<td class=f>")
		if row.value != "" && filterParam != "" {
			qs := cloneParams(params)
			qs.Set(filterParam, row.value)
			append("<a href='?", qs.Encode(), "' title='Filter by ", filterParam, " = ", row.value, "'>🔍</a>")
		}
		append("</td>")
		append("<th>")
		append("<div style='width: ", percentStr, "'", func() string {
			if row.value == "" {
				return " class=other"
			}
			return ""
		}(), "'></div>")
		label := row.value
		if label == "" {
			label = "Others"
		}
		append("<span title='", label, "'>", label, "</span>")
		append("</th>")
		append("<td>", formatNum(row.count), "</td>")
		append("<td class='pct'>", percentStr, "</td>")
		append("</tr>")
	}
	append("</table>")
	append("</div>")
}

type rowCount struct {
	value string
	count int64
}

func top10(ctx context.Context, db *sql.DB, column, where string, args []any) []rowCount {
	query := fmt.Sprintf(`WITH base_query AS (
		SELECT %s
		FROM stats
		WHERE %s
	),
	top_values AS (
		SELECT %s AS value, COUNT(*) AS count
		FROM base_query
		WHERE %s IS NOT NULL
		GROUP BY value
		ORDER BY count DESC
	),
	top_n AS (
		SELECT * FROM top_values ORDER BY count DESC LIMIT 10
	),
	others AS (
		SELECT NULL AS value, COUNT(*) AS count
		FROM base_query
		WHERE %s IS NOT NULL AND %s NOT IN (SELECT value FROM top_n)
	)
	SELECT * FROM top_n
	UNION ALL
	SELECT * FROM others
	WHERE count > 0`, column, where, column, column, column, column)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return readRows(rows)
}

func top10Uniq(ctx context.Context, db *sql.DB, column, where string, args []any) []rowCount {
	query := fmt.Sprintf(`WITH base_query AS (
		SELECT ANY_VALUE(%s) AS %s, MAX(mult) AS mult
		FROM stats
		WHERE %s
		GROUP BY uniq
	),
	top_values AS (
		SELECT %s AS value, SUM(mult) AS count
		FROM base_query
		WHERE %s IS NOT NULL
		GROUP BY value
		ORDER BY count DESC
	),
	top_n AS (
		SELECT * FROM top_values ORDER BY count DESC LIMIT 10
	),
	others AS (
		SELECT NULL AS value, SUM(mult) AS count
		FROM base_query
		WHERE %s IS NOT NULL AND %s NOT IN (SELECT value FROM top_n)
	)
	SELECT * FROM top_n
	UNION ALL
	SELECT * FROM others
	WHERE count > 0`, column, column, where, column, column, column, column)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return readRows(rows)
}

func readRows(rows *sql.Rows) []rowCount {
	var out []rowCount
	for rows.Next() {
		var value sql.NullString
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			continue
		}
		out = append(out, rowCount{value: value.String, count: count})
	}
	return out
}

func listDates(fromDate, toDate time.Time) []time.Time {
	var dates []time.Time
	for d := fromDate; !d.After(toDate); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d)
	}
	return dates
}

func roundMaxVal(maxVal int64) int64 {
	switch {
	case maxVal >= 200000:
		return roundTo(maxVal, 100000)
	case maxVal >= 20000:
		return roundTo(maxVal, 10000)
	case maxVal >= 2000:
		return roundTo(maxVal, 1000)
	case maxVal >= 100:
		return roundTo(maxVal, 100)
	default:
		return 100
	}
}

func roundTo(n, m int64) int64 {
	return ((n - 1) / m + 1) * m
}

func horizontalStep(maxVal int64) int64 {
	switch {
	case maxVal >= 600000:
		return 200000
	case maxVal >= 300000:
		return 100000
	case maxVal >= 100000:
		return 50000
	case maxVal >= 60000:
		return 20000
	case maxVal >= 30000:
		return 10000
	case maxVal >= 10000:
		return 5000
	case maxVal >= 6000:
		return 2000
	case maxVal >= 3000:
		return 1000
	case maxVal >= 1000:
		return 500
	case maxVal >= 600:
		return 200
	case maxVal >= 300:
		return 100
	case maxVal >= 100:
		return 50
	case maxVal >= 60:
		return 20
	default:
		return 10
	}
}

func formatNum(n int64) string {
	switch {
	case n >= 10000000:
		return strings.TrimSuffix(fmt.Sprintf("%.0fM", float64(n)/1000000.0), ".0")
	case n >= 1000000:
		return strings.TrimSuffix(fmt.Sprintf("%.1fM", float64(n)/1000000.0), ".0")
	case n >= 10000:
		return strings.TrimSuffix(fmt.Sprintf("%.0fK", float64(n)/1000.0), ".0")
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1fK", float64(n)/1000.0), ".0")
	default:
		return strconv.FormatInt(n, 10)
	}
}

func formatNumberWithCommas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func average(values map[time.Time]int64) int64 {
	if len(values) == 0 {
		return 0
	}
	var sum int64
	for _, v := range values {
		sum += v
	}
	return int64(float64(sum)/float64(len(values)) + 0.5)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func cloneParams(params url.Values) url.Values {
	out := url.Values{}
	for k, v := range params {
		out[k] = append([]string{}, v...)
	}
	return out
}
