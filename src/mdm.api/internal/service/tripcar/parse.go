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
	// Vendor names the company visited when the description holds several
	// "廠商：…" blocks, one per stop; empty for a plain single-block event.
	Vendor    string   `json:"vendor,omitempty"`
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

// roleSuffixRe strips a trailing role note such as "(管考)" or "（管考）" from
// a person's name.
var roleSuffixRe = regexp.MustCompile(`[（(][^）)]*[）)]\s*$`)

func cleanName(n string) string {
	return strings.TrimSpace(roleSuffixRe.ReplaceAllString(strings.TrimSpace(n), ""))
}

// isBlank: nothing, or only the dash placeholders a template leaves behind
// ("地點：—"). A field holding just a placeholder counts as not filled in.
func isBlank(v string) bool {
	return strings.Trim(strings.TrimSpace(v), "-–—－﹣─ \t　") == ""
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
		if p = strings.TrimSpace(p); !isBlank(p) {
			out = append(out, p)
		}
	}
	return out
}

// attendeeLabels name the lines that list who is going. 協會人員 is the
// summary-block template; 進廠人員 is what the factory-visit events use, where
// each person may have their own line.
var attendeeLabels = map[string]bool{"協會人員": true, "進廠人員": true}

// ParseDescription extracts the known "標籤：內容" lines. Unknown lines and
// lines without a label are ignored; a missing label just leaves its field
// empty.
//
// For 地點/時間/交通/信箱 the first occurrence wins: the summary block sits at
// the top and an itinerary below it may reuse those words for a single
// stop, which must not override the trip's own header. Attendee lines are the
// exception — several 進廠人員 lines are normal — so names from every
// attendee line are collected, de-duplicated, with role notes like "(管考)"
// removed.
func ParseDescription(desc string) Trip {
	return parseLines(strings.Split(stripHTML(desc), "\n"), false)
}

// parseLines reads one block of "標籤：內容" lines.
//
// Besides the labelled lines, an unlabelled line directly under 信箱 that
// holds mailboxes is a continuation of it (Google shows a second mailbox on
// its own line); a blank line or any other label ends the continuation.
//
// With addressIsLocation, 地址 counts as the destination too (the multi-vendor
// template has 地址 where the plain one has 地點); whichever comes first wins.
// It is off for plain descriptions, where "地址:" lines are usually stops in an
// itinerary rather than the trip's own destination.
func parseLines(lines []string, addressIsLocation bool) Trip {
	var t Trip
	seen := map[string]bool{}
	names := map[string]bool{}
	inMail := false // the previous line was the accepted 信箱 line
	for _, line := range lines {
		line = strings.TrimSpace(line)
		key, val, ok := splitLabel(line)
		if !ok {
			if inMail && strings.Contains(line, "@") {
				for _, m := range splitList(line) {
					if strings.Contains(m, "@") {
						t.Emails = append(t.Emails, m)
					}
				}
			} else {
				inMail = false
			}
			continue
		}
		inMail = false
		if addressIsLocation && key == "地址" {
			key = "地點"
		}
		if attendeeLabels[key] {
			for _, n := range splitList(val) {
				if n = cleanName(n); n != "" && !names[n] {
					names[n] = true
					t.Names = append(t.Names, n)
				}
			}
			continue
		}
		if seen[key] {
			continue
		}
		if isBlank(val) {
			val = "" // a placeholder is "not filled in", but still the header's own line
		}
		switch key {
		case "地點":
			t.Location = val
		case "時間":
			t.Time = val
		case "交通":
			t.Transport = val
		case "信箱":
			t.Emails = splitList(val)
			inMail = true
		case "廠商":
			t.Vendor = val
		default:
			continue
		}
		seen[key] = true
	}
	return t
}

// ParseStops splits a description into its stops. A description with
// "廠商：" lines has one block per vendor (each running to the next 廠商 line),
// every block with its own 地址／時間／交通／協會人員／信箱; anything before the
// first 廠商 line is not part of a stop. Without 廠商 lines the whole
// description is a single stop, read exactly like ParseDescription.
func ParseStops(desc string) []Trip {
	lines := strings.Split(stripHTML(desc), "\n")
	var starts []int
	for i, l := range lines {
		if k, _, ok := splitLabel(strings.TrimSpace(l)); ok && k == "廠商" {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return []Trip{parseLines(lines, false)}
	}
	stops := make([]Trip, 0, len(starts))
	for n, from := range starts {
		to := len(lines)
		if n+1 < len(starts) {
			to = starts[n+1]
		}
		stops = append(stops, parseLines(lines[from:to], true))
	}
	return stops
}

// RequiredFields are the labels every business-trip event must fill in, in the
// order they appear in the template.
var RequiredFields = []string{"地點", "時間", "交通", "協會人員", "信箱"}

// Missing lists the required fields that are empty (or only a placeholder),
// in template order. An event is only mailed about when this is empty, so a
// half-filled event is reported rather than guessed at.
func (t Trip) Missing() []string {
	var m []string
	for _, f := range RequiredFields {
		var filled bool
		switch f {
		case "地點":
			filled = t.Location != ""
		case "時間":
			filled = t.Time != ""
		case "交通":
			filled = t.Transport != ""
		case "協會人員":
			filled = len(t.Names) > 0
		case "信箱":
			filled = len(t.Emails) > 0
		}
		if !filled {
			m = append(m, f)
		}
	}
	return m
}

var bracketRe = regexp.MustCompile(`[（(]([^）)]*)[）)]`)

// LocationFromTitle returns the bracketed parts of an event title, e.g.
// "76工危品進廠(臺南分局)" -> "臺南分局". Factory-visit events carry their
// destination only there. Only the brackets are used, not the whole title, so
// project names and company names in the title don't count as places.
func LocationFromTitle(title string) string {
	var parts []string
	for _, m := range bracketRe.FindAllStringSubmatch(title, -1) {
		if p := strings.TrimSpace(m[1]); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "、")
}
