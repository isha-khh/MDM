package tripcar

import (
	"reflect"
	"testing"
	"time"
)

func bp(b bool) *bool { return &b }

func TestParseDescription(t *testing.T) {
	plain := "地點： 苗栗、台中\n時間：08:00-17:00\n交通：高鐵＋租車\n協會人員：張啓瑞 、劉名政、吳宜蓁\n信箱：leonchang@mail.isha.org.tw、foylaou0326@mail.isha.org.tw、idsl5397@mail.isha.org.tw"

	// Google Calendar's web editor saves the same text as HTML.
	rich := "<b>地點：</b> 苗栗、台中<br>時間：08:00-17:00<br>交通：高鐵＋租車<br>協會人員：張啓瑞&nbsp;、劉名政、吳宜蓁<br>" +
		"信箱：<a href=\"mailto:leonchang@mail.isha.org.tw\">leonchang@mail.isha.org.tw</a>、foylaou0326@mail.isha.org.tw、idsl5397@mail.isha.org.tw"

	want := Trip{
		Location:  "苗栗、台中",
		Time:      "08:00-17:00",
		Transport: "高鐵＋租車",
		Names:     []string{"張啓瑞", "劉名政", "吳宜蓁"},
		Emails:    []string{"leonchang@mail.isha.org.tw", "foylaou0326@mail.isha.org.tw", "idsl5397@mail.isha.org.tw"},
	}
	for name, in := range map[string]string{"plain": plain, "html": rich} {
		if got := ParseDescription(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}

// A real description from the shared calendar (company names and addresses
// replaced): HTML line breaks, a separator, then an itinerary whose lines
// contain half- and full-width colons and times.
const realTripDescription = `地點： 苗栗、台中<br>時間：08:00-17:00<br>交通：高鐵＋租車<br>協會人員：張啓瑞 、劉名政、吳宜蓁<br>` +
	`信箱：leonchang@mail.isha.org.tw、foylaou0326@mail.isha.org.tw、idsl5397@mail.isha.org.tw<br>` +
	`--------------------------------------<br><br>` +
	`<ol><li> 09:00-12:00：公司甲、公司乙（苗栗縣三義鄉西湖村伯公坑40之7號1樓）</li>` +
	`<li>14:00-15:30：公司丙　（臺中市大雅區秀山里雅秀一路39號)</li></ol>`

func TestParseDescription_RealWorldSample(t *testing.T) {
	got := ParseDescription(realTripDescription)
	want := Trip{
		Location:  "苗栗、台中",
		Time:      "08:00-17:00",
		Transport: "高鐵＋租車",
		Names:     []string{"張啓瑞", "劉名政", "吳宜蓁"},
		Emails:    []string{"leonchang@mail.isha.org.tw", "foylaou0326@mail.isha.org.tw", "idsl5397@mail.isha.org.tw"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// Miaoli + Taichung: parsed fine, but not a destination that needs a car from Kaohsiung.
	if LocationMatches(got.Location, DefaultTargetCounties) {
		t.Error("the sample trip must not match the target counties")
	}
}

func TestParseDescription_FirstLabelWinsOverLaterItineraryLines(t *testing.T) {
	// An itinerary stop that happens to be labelled 地點 / 時間 / 信箱 must not
	// replace the trip's header (otherwise a Miaoli trip could turn into a match).
	d := realTripDescription + `<br>地點：高雄市前鎮區<br>時間：下午<br>信箱：someone-else@example.com`
	got := ParseDescription(d)
	if got.Location != "苗栗、台中" || got.Time != "08:00-17:00" || len(got.Emails) != 3 {
		t.Errorf("later labels overrode the header: %+v", got)
	}
}

func TestParseDescription_Tolerant(t *testing.T) {
	// ASCII colon, unknown lines, no labels at all, empty input.
	got := ParseDescription("備註 隨便寫\n地點:台南\n交通 : 租車\n沒有冒號的一行")
	if got.Location != "台南" || got.Transport != "租車" {
		t.Errorf("ascii colon not handled: %+v", got)
	}
	if got := ParseDescription(""); !reflect.DeepEqual(got, Trip{}) {
		t.Errorf("empty input should give zero Trip, got %+v", got)
	}
	if got := ParseDescription("只是一般的說明文字"); !reflect.DeepEqual(got, Trip{}) {
		t.Errorf("unlabelled text should give zero Trip, got %+v", got)
	}
}

func TestLocationMatches(t *testing.T) {
	cases := []struct {
		loc  string
		want bool
	}{
		{"台南", true},
		{"臺南市", true},      // 臺 vs 台
		{"苗栗、台中、高雄", true}, // any one of several
		{"屏東縣恆春鎮", true},
		{"苗栗、台中", false}, // the sample trip: not a target
		{"", false},
		{"台北", false},
	}
	for _, c := range cases {
		if got := LocationMatches(c.loc, DefaultTargetCounties); got != c.want {
			t.Errorf("LocationMatches(%q) = %v, want %v", c.loc, got, c.want)
		}
	}
	if LocationMatches("台南", nil) {
		t.Error("no targets configured should never match")
	}
}

// Modelled on the real directory API: personal accounts with mail,
// some without mail, and a duplicate-name pair.
func testUsers() []User {
	return []User{
		{SamAccountName: "leonchang", DisplayName: "26安環處-高雄-張啓瑞", Mail: "leonchang@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "foy", DisplayName: "26安環處-高雄-劉名政", Mail: "foylaou0326@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(true)},
		{SamAccountName: "idsl5397", DisplayName: "26安環處-高雄-吳宜蓁", Mail: "idsl5397@mail.isha.org.tw", HasDriverLicense: bp(false), HasOwnCar: bp(false)},
		{SamAccountName: "ycwu", DisplayName: "26安環處-高雄-吳郁君", Mail: "ycwu@mail.isha.org.tw"}, // nothing recorded yet
		{SamAccountName: "jjian0225", DisplayName: "26安環處-高雄-簡維江", Mail: "jjian0225@mail.isha.org.tw", HasDriverLicense: bp(true)},
		{SamAccountName: "jjjian0225", DisplayName: "簡維江"}, // same person, second account without mail
		{SamAccountName: "nomail", DisplayName: "26安環處-高雄-無信箱", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
	}
}

func decisions(r Result) map[string]Decision {
	m := map[string]Decision{}
	for _, c := range r.Candidates {
		key := c.Email
		if c.User != nil {
			key = c.User.SamAccountName
		} else if key == "" {
			key = c.Name
		}
		m[key] = c.Decision
	}
	return m
}

func TestEvaluate_NotApplicableOutsideTargets(t *testing.T) {
	tr := Trip{Location: "苗栗、台中", Emails: []string{"leonchang@mail.isha.org.tw"}}
	if r := Evaluate(tr, testUsers(), DefaultTargetCounties); r.Applicable || len(r.Candidates) != 0 {
		t.Errorf("trip outside target counties should be ignored, got %+v", r)
	}
}

func TestEvaluate_LicenseDecisions(t *testing.T) {
	tr := Trip{
		Location: "高雄、屏東",
		Emails:   []string{"leonchang@mail.isha.org.tw", "idsl5397@mail.isha.org.tw", "ycwu@mail.isha.org.tw", "ghost@mail.isha.org.tw"},
	}
	r := Evaluate(tr, testUsers(), DefaultTargetCounties)
	if !r.Applicable {
		t.Fatal("expected applicable")
	}
	got := decisions(r)
	want := map[string]Decision{
		"leonchang":              Notify,      // licensed
		"idsl5397":               NoLicense,   // explicitly no license
		"ycwu":                   MissingData, // not filled in: reported, not mailed
		"ghost@mail.isha.org.tw": Unmatched,   // not in directory
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEvaluate_MailIsCaseInsensitive(t *testing.T) {
	tr := Trip{Location: "台南", Emails: []string{"LeonChang@Mail.ISHA.org.tw"}}
	r := Evaluate(tr, testUsers(), DefaultTargetCounties)
	if got := decisions(r)["leonchang"]; got != Notify {
		t.Errorf("mixed-case mail should still match, got %v", got)
	}
}

func TestEvaluate_NameFallbackAndDedup(t *testing.T) {
	// 張啓瑞 appears both by mail and by name: one candidate, not two.
	// 無信箱 has no mail in the directory, so only the name can find them.
	tr := Trip{
		Location: "台南",
		Names:    []string{"張啓瑞", "無信箱"},
		Emails:   []string{"leonchang@mail.isha.org.tw"},
	}
	r := Evaluate(tr, testUsers(), DefaultTargetCounties)
	if len(r.Candidates) != 2 {
		t.Fatalf("want 2 candidates (deduped), got %d: %+v", len(r.Candidates), r.Candidates)
	}
	got := decisions(r)
	if got["leonchang"] != Notify || got["nomail"] != Notify {
		t.Errorf("unexpected decisions: %v", got)
	}
}

func TestEvaluate_AmbiguousNameIsNotGuessed(t *testing.T) {
	// Two accounts contain 簡維江 and no mail confirms which one.
	r := Evaluate(Trip{Location: "高雄", Names: []string{"簡維江"}}, testUsers(), DefaultTargetCounties)
	if len(r.Candidates) != 1 || r.Candidates[0].Decision != Unmatched {
		t.Errorf("ambiguous name should be Unmatched, got %+v", r.Candidates)
	}

	// ...but if the mail pinned one account down, the name is already covered.
	r = Evaluate(Trip{Location: "高雄", Names: []string{"簡維江"}, Emails: []string{"jjian0225@mail.isha.org.tw"}}, testUsers(), DefaultTargetCounties)
	if len(r.Candidates) != 1 || r.Candidates[0].Decision != Notify {
		t.Errorf("mail should resolve the ambiguity, got %+v", r.Candidates)
	}
}

func TestSortByPriority(t *testing.T) {
	c := []Candidate{
		{Name: "owner", User: &User{HasOwnCar: bp(true)}},
		{Name: "unknown", User: &User{HasOwnCar: nil}},
		{Name: "nocar-1", User: &User{HasOwnCar: bp(false)}},
		{Name: "unmatched"},
		{Name: "nocar-2", User: &User{HasOwnCar: bp(false)}},
	}
	SortByPriority(c)
	var got []string
	for _, x := range c {
		got = append(got, x.Name)
	}
	want := []string{"nocar-1", "nocar-2", "unknown", "owner", "unmatched"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (stable among equals)", got, want)
	}
}

func TestDueForNotice(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Taipei")
	d := func(m time.Month, day, hour int) time.Time { return time.Date(2026, m, day, hour, 0, 0, 0, loc) }
	start := d(10, 20, 8) // trip starts Oct 20

	cases := []struct {
		name  string
		today time.Time
		sent  bool
		want  bool
	}{
		{"8 days out: too early", d(10, 12, 9), false, false},
		{"exactly 7 days out", d(10, 13, 9), false, true},
		{"7 days out, late evening", d(10, 13, 23), false, true},
		{"missed the exact day, 3 days out", d(10, 17, 9), false, true},
		{"event added 1 day before", d(10, 19, 9), false, true},
		{"day of the trip", d(10, 20, 9), false, false},
		{"after the trip started", d(10, 21, 9), false, false},
		{"already sent", d(10, 14, 9), true, false},
	}
	for _, c := range cases {
		if got := DueForNotice(start, c.today, c.sent, loc); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDueForNotice_UsesLocalCalendarDay(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Taipei")
	// 23:30 UTC on Oct 12 is already Oct 13 in Taipei: the notice day for an
	// Oct 20 trip. A UTC-based date would wrongly say "not yet".
	today := time.Date(2026, 10, 12, 23, 30, 0, 0, time.UTC)
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, loc)
	if !DueForNotice(start, today, false, loc) {
		t.Error("should be due: it is already Oct 13 in Taipei")
	}
}

// Factory-visit events ("76工危品進廠(…分局)") are laid out differently from
// the 地點/信箱 template. These are real descriptions with company names
// replaced.
func TestParseDescription_FactoryVisitStyle(t *testing.T) {
	cases := []struct {
		name string
		desc string
		want []string
	}{
		{
			"single label, html table itinerary",
			`進廠人員:羅羿昇<br><br><table dir="ltr"><tbody><tr><td><br>10/27 屏南<br>10:30-12:00公司A</td></tr><tr><td>14:00-15:30公司B屏南廠</td></tr></tbody></table>`,
			[]string{"羅羿昇"},
		},
		{
			"several labelled lines, role notes stripped, plain text",
			"進廠人員:夏宗瑜(管考)\n\n10:00-11:30\n公司A\n\n進廠人員:郭人銓（管考）\n13:00-14:30\n公司B\n\n15:10-16:30 羅羿昇\n公司C",
			[]string{"夏宗瑜", "郭人銓"}, // the unlabelled inline name is not picked up
		},
		{
			"same person listed twice",
			"進廠人員:陳小明\n10:30-12:00 公司A<br>進廠人員:陳小明",
			[]string{"陳小明"},
		},
		{
			"several people on one line",
			"進廠人員:甲、乙 丙",
			[]string{"甲", "乙", "丙"},
		},
		{
			"no attendee line at all",
			"05 2211526#216-許課長\n地址:嘉義縣民雄鄉興南村工業一路10號\n時間:1:30~3:30\n出席:卓技師",
			nil,
		},
	}
	for _, c := range cases {
		got := ParseDescription(c.desc).Names
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: names %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseDescription_TimeRangesAndAddressLinesAreNotLabels(t *testing.T) {
	// "10:30-12:00公司" and "地址:…" contain colons but are not trip labels.
	got := ParseDescription("10/27 屏南\n10:30-12:00聯友金屬\n地址:嘉義縣民雄鄉\n時間:1:30~3:30")
	if got.Location != "" || got.Transport != "" || len(got.Emails) != 0 {
		t.Errorf("only known labels count: %+v", got)
	}
	if got.Time != "1:30~3:30" { // a real "時間:" label is kept
		t.Errorf("time: %q", got.Time)
	}
}

func TestLocationFromTitle(t *testing.T) {
	cases := map[string]string{
		"76工危品進廠(臺南分局)":      "臺南分局",
		"76工危品進廠(高屏分局)":      "高屏分局",
		"76工危品進廠（臺中分局）":      "臺中分局",
		"[72FTIS]公司A(張三、李四)": "張三、李四",
		"上評第二次輔導":            "",
		"A(甲)B（乙）":           "甲、乙",
		"空括號()":              "",
		"":                   "",
	}
	for title, want := range cases {
		if got := LocationFromTitle(title); got != want {
			t.Errorf("LocationFromTitle(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestLocationMatches_GaopingShorthand(t *testing.T) {
	cases := []struct {
		loc  string
		want bool
	}{
		{"高屏分局", true}, // 高雄 + 屏東
		{"臺南分局", true},
		{"臺中分局", false},
		{"嘉義", false},
	}
	for _, c := range cases {
		if got := LocationMatches(c.loc, DefaultTargetCounties); got != c.want {
			t.Errorf("LocationMatches(%q) = %v, want %v", c.loc, got, c.want)
		}
	}
	// With only 台南 as a target, the shorthand must not match.
	if LocationMatches("高屏分局", []string{"台南"}) {
		t.Error("高屏 should expand to 高雄/屏東 only, not match an unrelated target")
	}
}
