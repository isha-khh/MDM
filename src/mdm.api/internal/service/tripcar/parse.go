// Package tripcar decides who should be told to book a rental car for an
// upcoming business trip, based on a calendar event's description and the
// directory (AD) data about each attendee. Pure logic only — no Google
// Calendar, AD or mail I/O — so every rule is unit-testable on its own.
package tripcar

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Trip is the structured form of a calendar event's free-text description,
// e.g.
//
//	地點： 苗栗、台中
//	時間：08:00-17:00
//	交通：高鐵＋租車
//	協會人員：張啓瑞 、劉名政
//	信箱：a@x.org、b@x.org
type Trip struct {
	Location  string   `json:"location"`
	Time      string   `json:"time"`
	Transport string   `json:"transport"`
	Names     []string `json:"names"`
	Emails    []string `json:"emails"`
}

var (
	lineBreakRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`)
	tagRe       = regexp.MustCompile(`<[^>]*>`)
)

// Google Calendar stores descriptions edited in the web UI as HTML
// (<br>, <b>, &nbsp; ...), so normalize to plain lines before parsing.
func stripHTML(s string) string {
	s = lineBreakRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.ReplaceAll(s, " ", " ")
}

func splitLabel(line string) (key, val string, ok bool) {
	idx := strings.IndexAny(line, ":：")
	if idx < 0 {
		return "", "", false
	}
	_, width := utf8.DecodeRuneInString(line[idx:])
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+width:]), true
}

func splitList(v string) []string {
	parts := strings.FieldsFunc(v, func(r rune) bool {
		switch r {
		case '、', '，', ',', '；', ';', ' ', '\t', '　':
			return true
		}
		return false
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseDescription extracts the known "標籤：內容" lines. Unknown lines and
// lines without a label are ignored; a missing label just leaves its field
// empty.
func ParseDescription(desc string) Trip {
	var t Trip
	for _, line := range strings.Split(stripHTML(desc), "\n") {
		key, val, ok := splitLabel(strings.TrimSpace(line))
		if !ok {
			continue
		}
		switch key {
		case "地點":
			t.Location = val
		case "時間":
			t.Time = val
		case "交通":
			t.Transport = val
		case "協會人員":
			t.Names = splitList(val)
		case "信箱":
			t.Emails = splitList(val)
		}
	}
	return t
}
