package tripcar

import (
	"sort"
	"strings"
	"time"
)

// NoticeLeadDays is how many days before departure the "please book a car"
// mail goes out.
const NoticeLeadDays = 7

// DefaultTargetCounties are the trip destinations that need a rental car
// arranged from the Kaohsiung office.
var DefaultTargetCounties = []string{"台南", "高雄", "屏東"}

// User mirrors the directory API's user record. HasDriverLicense and
// HasOwnCar are tri-state: nil means "not filled in yet", which is common
// until the directory data is completed.
type User struct {
	SamAccountName   string
	DisplayName      string
	Mail             string
	Disabled         bool // a disabled account is never matched or notified
	HasDriverLicense *bool
	HasOwnCar        *bool
}

type Decision string

const (
	Notify      Decision = "notify"       // licensed attendee: send the booking notice
	MissingData Decision = "missing_data" // license status not recorded yet: report, don't mail
	NoLicense   Decision = "no_license"   // explicitly unlicensed: never mailed
	Unmatched   Decision = "unmatched"    // attendee not found in the directory
)

type Candidate struct {
	Name     string
	Email    string
	User     *User
	Decision Decision
}

type Result struct {
	// Applicable is false when the destination isn't one of the target
	// counties; Candidates is then empty and nothing needs to happen.
	Applicable bool
	Candidates []Candidate
}

// 台/臺 are used interchangeably in place names; compare on 台.
func normalizePlace(s string) string {
	s = strings.ReplaceAll(s, "臺", "台")
	// 高屏 is the usual shorthand for Kaohsiung + Pingtung (e.g. "高屏分局").
	return strings.ReplaceAll(s, "高屏", "高雄屏東")
}

// LocationMatches reports whether the location text mentions any target
// county. Matching is a substring check on the county name, so "台南市",
// "臺南" and "台中、台南" all count for "台南".
func LocationMatches(location string, targets []string) bool {
	loc := normalizePlace(location)
	for _, t := range targets {
		if t = normalizePlace(strings.TrimSpace(t)); t != "" && strings.Contains(loc, t) {
			return true
		}
	}
	return false
}

func findByMail(users []User, mail string) *User {
	for i := range users {
		if !users[i].Disabled && users[i].Mail != "" && strings.EqualFold(users[i].Mail, mail) {
			return &users[i]
		}
	}
	return nil
}

// Directory display names look like "26安環處-高雄-林雋哲", so a person's
// plain name is matched as a substring. The directory often also holds an
// older short account for the same person ("林雋哲" with no mail, next to
// "26安環處-高雄-林雋哲" with one): when a name matches several accounts, the
// ones with a mail address are the real personal accounts, so if exactly one
// of those remains it is the match. Otherwise there is more than one
// candidate and none is guessed. Disabled accounts never match.
func findByName(users []User, name string) []*User {
	var all, withMail []*User
	if name == "" {
		return nil
	}
	for i := range users {
		if users[i].Disabled || !strings.Contains(users[i].DisplayName, name) {
			continue
		}
		all = append(all, &users[i])
		if users[i].Mail != "" {
			withMail = append(withMail, &users[i])
		}
	}
	if len(all) > 1 && len(withMail) == 1 {
		return withMail
	}
	return all
}

func decide(u *User) Decision {
	switch {
	case u.HasDriverLicense == nil:
		return MissingData
	case !*u.HasDriverLicense:
		return NoLicense
	default:
		return Notify
	}
}

// Evaluate applies the trip rules: destination must be a target county, and
// each attendee needs a recorded driver's license to be notified. Attendees
// are resolved by e-mail first, then by name for anyone the mail list didn't
// cover; a person reachable both ways is only listed once.
func Evaluate(t Trip, users []User, targets []string) Result {
	if !LocationMatches(t.Location, targets) {
		return Result{}
	}
	res := Result{Applicable: true}
	seen := map[string]bool{}

	// The template lists names and mailboxes in the same order, so when both
	// lists have the same length they are read as pairs and the mailbox alone
	// decides who the person is; the name is only a label. Two people with the
	// same name are told apart by their mailboxes, and a mailbox the directory
	// doesn't know is reported as not found rather than guessed from the name.
	paired := len(t.Names) > 0 && len(t.Names) == len(t.Emails)
	// With at least as many mailboxes as names, every person is identified by a
	// mailbox and names are never looked up on their own.
	byMailOnly := len(t.Emails) > 0 && len(t.Names) <= len(t.Emails)

	for i, email := range t.Emails {
		name := ""
		if paired {
			name = t.Names[i]
		}
		u := findByMail(users, email)
		if u == nil {
			res.Candidates = append(res.Candidates, Candidate{Name: name, Email: email, Decision: Unmatched})
			continue
		}
		if seen[u.SamAccountName] {
			continue
		}
		seen[u.SamAccountName] = true
		res.Candidates = append(res.Candidates, Candidate{Name: name, Email: email, User: u, Decision: decide(u)})
	}

	for _, name := range t.Names {
		if byMailOnly {
			break
		}
		matches := findByName(users, name)
		covered := false
		for _, m := range matches {
			if seen[m.SamAccountName] {
				covered = true
			}
		}
		if covered {
			continue
		}
		switch len(matches) {
		case 0:
			res.Candidates = append(res.Candidates, Candidate{Name: name, Decision: Unmatched})
		case 1:
			seen[matches[0].SamAccountName] = true
			res.Candidates = append(res.Candidates, Candidate{Name: name, Email: matches[0].Mail, User: matches[0], Decision: decide(matches[0])})
		default:
			// Same name, several accounts, none confirmed by e-mail: don't
			// guess which one is the traveller.
			res.Candidates = append(res.Candidates, Candidate{Name: name, Decision: Unmatched})
		}
	}
	return res
}

// PairingProblem reports people listed without a mailbox of their own: more
// names than mailboxes (and at least one mailbox). Mailboxes are what identify
// a person — it is how two people of the same name are told apart — so a name
// with no mailbox can't be trusted. Extra mailboxes, on the other hand, are
// harmless: each one identifies someone by itself.
func (t Trip) PairingProblem() bool {
	return len(t.Emails) > 0 && len(t.Names) > len(t.Emails)
}

// PriorityRank orders people for rental-car approval: those without a car of
// their own come first, unknown next, owners last. Lower sorts earlier.
func PriorityRank(u *User) int {
	switch {
	case u == nil:
		return 3
	case u.HasOwnCar == nil:
		return 1
	case !*u.HasOwnCar:
		return 0
	default:
		return 2
	}
}

// SortByPriority orders candidates by PriorityRank, keeping the original
// order among equals.
func SortByPriority(c []Candidate) {
	sort.SliceStable(c, func(i, j int) bool {
		return PriorityRank(c[i].User) < PriorityRank(c[j].User)
	})
}

func dateOnly(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// DueForNotice reports whether the notice should be sent today: from
// NoticeLeadDays before the trip starts up to (not including) the start day,
// and only if it hasn't been sent yet. A window rather than an exact day, so
// a missed daily run, or an event added less than a week out, still gets
// its notice.
func DueForNotice(start, today time.Time, alreadySent bool, loc *time.Location) bool {
	if alreadySent {
		return false
	}
	s := dateOnly(start, loc)
	t := dateOnly(today, loc)
	return !t.Before(s.AddDate(0, 0, -NoticeLeadDays)) && t.Before(s)
}
