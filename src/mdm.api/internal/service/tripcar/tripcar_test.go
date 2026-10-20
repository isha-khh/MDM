package tripcar

import (
	"reflect"
	"strings"
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

func TestEvaluate_DuplicateAccountWithoutMailDoesNotMakeANameAmbiguous(t *testing.T) {
	// Two accounts contain 簡維江, but only one has a mail address: it is the
	// real personal account, so the name resolves to it. (Two people who both
	// have mail stay ambiguous — see the directory-shaped tests below.)
	r := Evaluate(Trip{Location: "高雄", Names: []string{"簡維江"}}, testUsers(), DefaultTargetCounties)
	if len(r.Candidates) != 1 || r.Candidates[0].Decision != Notify || r.Candidates[0].User.SamAccountName != "jjian0225" {
		t.Errorf("got %+v", r.Candidates)
	}

	// A mail address in the event pins the account down directly, and the
	// name that points at the same person isn't listed again.
	r = Evaluate(Trip{Location: "高雄", Names: []string{"簡維江"}, Emails: []string{"jjian0225@mail.isha.org.tw"}}, testUsers(), DefaultTargetCounties)
	if len(r.Candidates) != 1 || r.Candidates[0].Decision != Notify {
		t.Errorf("mail should resolve it once, got %+v", r.Candidates)
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

// Shaped like the real directory: personal accounts with a mail address and
// department, older short accounts without, service accounts, and tri-state
// license/car values still being filled in.
func directoryLikeUsers() []User {
	return []User{
		{SamAccountName: "idsl5397", DisplayName: "26安環處-高雄-吳宜蓁", Mail: "idsl5397@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "ids", DisplayName: "吳宜蓁"},                                              // old short account, no mail
		{SamAccountName: "s01023", DisplayName: "26安環處-高雄-李念芷", Mail: "s01023@mail.isha.org.tw"}, // license not filled in yet
		{SamAccountName: "s01", DisplayName: "李念芷"},
		{SamAccountName: "yishengluo1006", DisplayName: "26安環處-高雄-羅羿昇", Mail: "yishengluo1006@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "zoey.xia", DisplayName: "26安環處-高雄-夏宗瑜", Mail: "zoey.xia@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(true)},
		{SamAccountName: "a403109970", DisplayName: "26安環處-高雄-郭人銓", Mail: "a403109970@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "jjian0225", DisplayName: "26安環處-高雄-簡維江", Mail: "jjian0225@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "jjjian0225", DisplayName: "簡維江"},
		{SamAccountName: "lcc1077", DisplayName: "26安環處-高雄-林雋哲", Mail: "lcc1077@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "lcc", DisplayName: "林雋哲"},
		{SamAccountName: "oldhand", DisplayName: "26安環處-高雄-離職者", Mail: "oldhand@mail.isha.org.tw", Disabled: true, HasDriverLicense: bp(true)},
		{SamAccountName: "tw1", DisplayName: "26安環處-高雄-王小明", Mail: "wang1@mail.isha.org.tw", HasDriverLicense: bp(true)},
		{SamAccountName: "tw2", DisplayName: "26安環處-台南-王小明", Mail: "wang2@mail.isha.org.tw", HasDriverLicense: bp(true)}, // two real people, same name
	}
}

func TestEvaluate_NameWithOldShortAccountResolvesToThePersonalOne(t *testing.T) {
	// 進廠人員 lines give names only. 吳宜蓁 matches both the personal account and
	// an old mail-less short account: the one with a mail address wins.
	r := Evaluate(Trip{Location: "高屏分局", Names: []string{"吳宜蓁", "羅羿昇", "簡維江", "林雋哲"}}, directoryLikeUsers(), DefaultTargetCounties)
	got := decisions(r)
	for acct, want := range map[string]Decision{"idsl5397": Notify, "yishengluo1006": Notify, "jjian0225": Notify, "lcc1077": Notify} {
		if got[acct] != want {
			t.Errorf("%s: got %v, want %v (all: %v)", acct, got[acct], want, got)
		}
	}
	if len(r.Candidates) != 4 {
		t.Errorf("each person once, not once per account: %+v", r.Candidates)
	}
}

func TestEvaluate_TwoRealPeopleWithTheSameNameStayAmbiguous(t *testing.T) {
	r := Evaluate(Trip{Location: "台南", Names: []string{"王小明"}}, directoryLikeUsers(), DefaultTargetCounties)
	if len(r.Candidates) != 1 || r.Candidates[0].Decision != Unmatched || r.Candidates[0].User != nil {
		t.Errorf("two accounts with mail share the name, so none may be guessed: %+v", r.Candidates)
	}
}

func TestEvaluate_DisabledAccountsAreNeverMatched(t *testing.T) {
	byMail := Evaluate(Trip{Location: "高雄", Emails: []string{"oldhand@mail.isha.org.tw"}}, directoryLikeUsers(), DefaultTargetCounties)
	byName := Evaluate(Trip{Location: "高雄", Names: []string{"離職者"}}, directoryLikeUsers(), DefaultTargetCounties)
	for name, r := range map[string]Result{"by mail": byMail, "by name": byName} {
		if len(r.Candidates) != 1 || r.Candidates[0].Decision != Unmatched {
			t.Errorf("%s: a disabled account must not be notified: %+v", name, r.Candidates)
		}
	}
}

func TestEvaluate_NameOnlyEventWithMissingDirectoryData(t *testing.T) {
	// 李念芷's license isn't recorded yet: reported, not mailed.
	r := Evaluate(Trip{Location: "台南", Names: []string{"李念芷", "夏宗瑜"}}, directoryLikeUsers(), DefaultTargetCounties)
	got := decisions(r)
	if got["s01023"] != MissingData || got["zoey.xia"] != Notify {
		t.Errorf("got %v", got)
	}
	SortByPriority(r.Candidates)
	// 李念芷: car unknown (rank 1); 夏宗瑜 owns a car (rank 2) -> unknown first.
	if r.Candidates[0].User.SamAccountName != "s01023" || r.Candidates[1].User.SamAccountName != "zoey.xia" {
		t.Errorf("priority order: %+v", r.Candidates)
	}
}

func TestTrip_MissingRequiredFields(t *testing.T) {
	full := "地點：臺南\n時間：09:00-12:00\n交通：高鐵＋租車\n協會人員：羅羿昇\n信箱：a@x.org"
	if m := ParseDescription(full).Missing(); len(m) != 0 {
		t.Errorf("complete description reported missing %v", m)
	}
	cases := []struct {
		name, desc string
		want       []string
	}{
		{"empty", "", []string{"地點", "時間", "交通", "協會人員", "信箱"}},
		{"labels with nothing after them", "地點：\n時間：\n交通：\n協會人員：\n信箱：", []string{"地點", "時間", "交通", "協會人員", "信箱"}},
		{"template placeholders", "地點：—\n時間：—\n交通：—\n協會人員：—\n信箱：—", []string{"地點", "時間", "交通", "協會人員", "信箱"}},
		{"other dash styles and spacing", "地點： - \n時間：－－\n交通：–\n協會人員：　—　\n信箱：—、—", []string{"地點", "時間", "交通", "協會人員", "信箱"}},
		{"one gap", "地點：臺南\n時間：09:00\n協會人員：羅羿昇\n信箱：a@x.org", []string{"交通"}},
		{"factory-visit attendee label counts as attendees", "地點：臺南\n時間：09:00\n交通：自駕\n進廠人員：羅羿昇\n信箱：a@x.org", nil},
	}
	for _, c := range cases {
		got := ParseDescription(c.desc).Missing()
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: missing %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseDescription_PlaceholderHeaderIsNotOverriddenByLaterItinerary(t *testing.T) {
	// An empty header line still is the header: a later itinerary "地點" must not fill it in.
	tr := ParseDescription("地點：—\n時間：09:00\n交通：自駕\n協會人員：羅羿昇\n信箱：a@x.org\n行程\n地點：某公司")
	if tr.Location != "" {
		t.Errorf("location %q", tr.Location)
	}
	if m := tr.Missing(); len(m) != 1 || m[0] != "地點" {
		t.Errorf("missing %v", m)
	}
}

func TestParseDescription_PlaceholderNamesAndEmailsAreDropped(t *testing.T) {
	tr := ParseDescription("協會人員：羅羿昇、—\n信箱：a@x.org、—")
	if len(tr.Names) != 1 || tr.Names[0] != "羅羿昇" || len(tr.Emails) != 1 {
		t.Errorf("%+v", tr)
	}
}

// The format the business settled on: half-width colons, addresses in the
// location, nicknames as names, and a mailbox for each.
const agreedFormat = `地點:經濟部園區管理局(高雄市前鎮區成功二路)、中油林園廠(高雄市林園區)
時間:1000-1200
交通:開車
協會人員:FOY、PIAU
信箱:foy@mail.isha.org.tw、piau@mail.isha.org.tw`

func TestAgreedFormat_ParsesAndIsComplete(t *testing.T) {
	tr := ParseDescription(agreedFormat)
	if m := tr.Missing(); len(m) != 0 {
		t.Fatalf("missing %v in %+v", m, tr)
	}
	if tr.Time != "1000-1200" || tr.Transport != "開車" || len(tr.Names) != 2 || len(tr.Emails) != 2 {
		t.Errorf("%+v", tr)
	}
	if !LocationMatches(tr.Location, DefaultTargetCounties) {
		t.Errorf("a Kaohsiung address should make it a target: %q", tr.Location)
	}
}

func TestEvaluate_NicknamesPairedWithMailboxes(t *testing.T) {
	users := []User{
		{SamAccountName: "foy", DisplayName: "26安環處-高雄-劉某", Mail: "foy@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "piau", DisplayName: "26安環處-高雄-張某", Mail: "piau@mail.isha.org.tw", HasDriverLicense: bp(false)},
	}
	res := Evaluate(ParseDescription(agreedFormat), users, DefaultTargetCounties)
	if len(res.Candidates) != 2 {
		t.Fatalf("each person once, no phantom 'not found' for the nicknames: %+v", res.Candidates)
	}
	c0, c1 := res.Candidates[0], res.Candidates[1]
	if c0.Name != "FOY" || c0.Decision != Notify || c1.Name != "PIAU" || c1.Decision != NoLicense {
		t.Errorf("%+v / %+v", c0, c1)
	}
}

func TestEvaluate_PairedMailboxNotInDirectoryIsReported(t *testing.T) {
	tr := ParseDescription(agreedFormat)
	res := Evaluate(tr, []User{{SamAccountName: "foy", Mail: "foy@mail.isha.org.tw", HasDriverLicense: bp(true)}}, DefaultTargetCounties)
	if len(res.Candidates) != 2 || res.Candidates[1].Decision != Unmatched || res.Candidates[1].Name != "PIAU" {
		t.Errorf("%+v", res.Candidates)
	}
}

func TestEvaluate_UnequalListsAreNotPaired(t *testing.T) {
	// Three names, two mailboxes: no way to tell who is who, so names are
	// looked up on their own as before.
	tr := Trip{Location: "高雄", Names: []string{"甲", "乙", "丙"}, Emails: []string{"a@x", "b@x"}}
	users := []User{
		{SamAccountName: "a", DisplayName: "甲", Mail: "a@x", HasDriverLicense: bp(true)},
		{SamAccountName: "c", DisplayName: "丙", Mail: "c@x", HasDriverLicense: bp(true)},
	}
	res := Evaluate(tr, users, DefaultTargetCounties)
	got := map[string]Decision{}
	for _, c := range res.Candidates {
		got[c.Name+"|"+c.Email] = c.Decision
	}
	if got["|a@x"] != Notify || got["|b@x"] != Unmatched || got["丙|c@x"] != Notify || got["乙|"] != Unmatched || len(res.Candidates) != 4 {
		t.Errorf("%+v", got)
	}
}

func TestEvaluate_MailboxDecidesNotTheName(t *testing.T) {
	// Two real people called 夏宗瑜; the mailbox says which one is travelling.
	users := []User{
		{SamAccountName: "a", DisplayName: "26安環處-高雄-夏宗瑜", Mail: "a@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(true)},
		{SamAccountName: "b", DisplayName: "27安環處-台北-夏宗瑜", Mail: "b@mail.isha.org.tw", HasDriverLicense: bp(false)},
	}
	tr := Trip{Location: "高雄", Names: []string{"夏宗瑜"}, Emails: []string{"b@mail.isha.org.tw"}}
	res := Evaluate(tr, users, DefaultTargetCounties)
	if len(res.Candidates) != 1 || res.Candidates[0].User.SamAccountName != "b" || res.Candidates[0].Decision != NoLicense {
		t.Errorf("%+v", res.Candidates)
	}
}

func TestEvaluate_TypoedMailboxIsNotRescuedByName(t *testing.T) {
	// The name is unique in the directory, but it might be a different person
	// from the traveller: never guess when the mailbox says otherwise.
	tr := Trip{Location: "高雄", Names: []string{"夏宗瑜"}, Emails: []string{"zoey.xai@mail.isha.org.tw"}}
	users := []User{{SamAccountName: "zoey.xia", DisplayName: "26安環處-高雄-夏宗瑜", Mail: "zoey.xia@mail.isha.org.tw", HasDriverLicense: bp(true)}}
	res := Evaluate(tr, users, DefaultTargetCounties)
	if len(res.Candidates) != 1 || res.Candidates[0].Decision != Unmatched || res.Candidates[0].User != nil {
		t.Errorf("%+v", res.Candidates)
	}
}

func TestTrip_PairingProblem(t *testing.T) {
	cases := []struct {
		names, emails int
		want          bool
	}{{2, 2, false}, {1, 1, false}, {3, 2, true}, {1, 2, false}, {0, 2, false}, {2, 0, false}}
	for _, c := range cases {
		tr := Trip{Names: make([]string, c.names), Emails: make([]string, c.emails)}
		if tr.PairingProblem() != c.want {
			t.Errorf("%d names / %d emails: got %v", c.names, c.emails, !c.want)
		}
	}
}

// A real description in the agreed format, verbatim.
const realAgreedSample = `地點:經濟部園區管理局(811高雄市楠梓區瑞屏里加昌路600號)、中油林園廠(832高雄市林園區五福里石化二路3號)
時間:1000-1200
交通:開車
協會人員:劉名政、吳宜蓁
信箱:foylaou0326@mail.isha.org.tw、idsl5397@mail.isha.org.tw`

func TestRealAgreedSample_Parse(t *testing.T) {
	tr := ParseDescription(realAgreedSample)
	if tr.Location != "經濟部園區管理局(811高雄市楠梓區瑞屏里加昌路600號)、中油林園廠(832高雄市林園區五福里石化二路3號)" {
		t.Errorf("location (addresses must stay whole, not be split on 、): %q", tr.Location)
	}
	if tr.Time != "1000-1200" || tr.Transport != "開車" {
		t.Errorf("time %q transport %q", tr.Time, tr.Transport)
	}
	if strings.Join(tr.Names, ",") != "劉名政,吳宜蓁" {
		t.Errorf("names %v", tr.Names)
	}
	if strings.Join(tr.Emails, ",") != "foylaou0326@mail.isha.org.tw,idsl5397@mail.isha.org.tw" {
		t.Errorf("emails %v", tr.Emails)
	}
	if m := tr.Missing(); len(m) != 0 || tr.PairingProblem() {
		t.Errorf("a complete event reported problems: missing %v pairing %v", m, tr.PairingProblem())
	}
}

func TestRealAgreedSample_AlsoParsesFromGoogleHTML(t *testing.T) {
	// Google's editor stores the same text as HTML.
	html := strings.ReplaceAll(realAgreedSample, "\n", "<br>")
	html = "<b>" + html + "</b>"
	a, b := ParseDescription(html), ParseDescription(realAgreedSample)
	if strings.Join(a.Names, ",") != strings.Join(b.Names, ",") || a.Location != b.Location || a.Time != b.Time || len(a.Missing()) != 0 {
		t.Errorf("html %+v vs plain %+v", a, b)
	}
}

func TestRealAgreedSample_TargetsByTheAddressCounty(t *testing.T) {
	loc := ParseDescription(realAgreedSample).Location
	if !LocationMatches(loc, DefaultTargetCounties) {
		t.Error("高雄市 in the addresses should make this a target trip")
	}
	// The same shape with a non-target address must not match.
	other := "某公司(404台中市北區某路1號)、某廠(苗栗縣竹南鎮某路2號)"
	if LocationMatches(other, DefaultTargetCounties) {
		t.Error("Taichung / Miaoli addresses are not targets")
	}
}

func TestRealAgreedSample_Evaluate(t *testing.T) {
	users := []User{
		{SamAccountName: "foylaou0326", DisplayName: "26安環處-高雄-劉名政", Mail: "foylaou0326@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)},
		{SamAccountName: "idsl5397", DisplayName: "26安環處-高雄-吳宜蓁", Mail: "idsl5397@mail.isha.org.tw", HasDriverLicense: bp(false), HasOwnCar: bp(false)},
		{SamAccountName: "ids", DisplayName: "吳宜蓁"}, // old short account of the same person, no mail
	}
	res := Evaluate(ParseDescription(realAgreedSample), users, DefaultTargetCounties)
	if !res.Applicable || len(res.Candidates) != 2 {
		t.Fatalf("%+v", res)
	}
	c0, c1 := res.Candidates[0], res.Candidates[1]
	if c0.Name != "劉名政" || c0.Decision != Notify || c0.User.SamAccountName != "foylaou0326" {
		t.Errorf("first: %+v", c0)
	}
	if c1.Name != "吳宜蓁" || c1.Decision != NoLicense || c1.User.SamAccountName != "idsl5397" {
		t.Errorf("second (explicitly no license, and the old account must not be picked): %+v", c1)
	}
}

// The multi-vendor event from the calendar: one block per vendor, 地址 for the
// destination, and a second mailbox on its own line under 信箱.
const multiVendorSample = `廠商：台灣瑞環
聯繫人：王秋琪 課長 06-6232280 #528
時間：0900-1200
地址：臺南市柳營區大農里環園西路二段19號
交通：開車
協會人員：夏宗瑜
信箱：zoey.xia@mail.isha.org.tw
isha.khh.owner@gmail.com

廠商：奇菱科技
聯繫人：蘇麗錦 課長 06-5890818 ext.17475
時間：1330-1700
地址：臺南市新市區豐華里王甲路1號
交通：開車
協會人員：夏宗瑜
信箱：zoey.xia@mail.isha.org.tw
isha.khh.owner@gmail.com`

func TestParseStops_MultiVendorSample(t *testing.T) {
	stops := ParseStops(multiVendorSample)
	if len(stops) != 2 {
		t.Fatalf("want 2 stops, got %d: %+v", len(stops), stops)
	}
	want := []struct{ vendor, loc, time string }{
		{"台灣瑞環", "臺南市柳營區大農里環園西路二段19號", "0900-1200"},
		{"奇菱科技", "臺南市新市區豐華里王甲路1號", "1330-1700"},
	}
	for i, w := range want {
		s := stops[i]
		if s.Vendor != w.vendor || s.Location != w.loc || s.Time != w.time || s.Transport != "開車" {
			t.Errorf("stop %d: %+v", i, s)
		}
		if strings.Join(s.Names, ",") != "夏宗瑜" || strings.Join(s.Emails, ",") != "zoey.xia@mail.isha.org.tw,isha.khh.owner@gmail.com" {
			t.Errorf("stop %d people: names %v emails %v", i, s.Names, s.Emails)
		}
		if m := s.Missing(); len(m) != 0 || s.PairingProblem() {
			t.Errorf("stop %d should be complete: missing %v pairing %v", i, m, s.PairingProblem())
		}
		if !LocationMatches(s.Location, DefaultTargetCounties) {
			t.Errorf("stop %d (臺南) should be a target", i)
		}
	}
}

func TestParseStops_ParsesGoogleHTMLToo(t *testing.T) {
	html := strings.ReplaceAll(multiVendorSample, "\n", "<br>")
	a, b := ParseStops(html), ParseStops(multiVendorSample)
	if len(a) != 2 || a[1].Vendor != b[1].Vendor || strings.Join(a[1].Emails, ",") != strings.Join(b[1].Emails, ",") {
		t.Errorf("%+v vs %+v", a, b)
	}
}

func TestParseStops_OneStopMissingAFieldIsReportedOnThatStop(t *testing.T) {
	stops := ParseStops(strings.Replace(multiVendorSample, "交通：開車\n協會人員：夏宗瑜\n信箱：zoey.xia@mail.isha.org.tw\nisha.khh.owner@gmail.com", "協會人員：夏宗瑜\n信箱：zoey.xia@mail.isha.org.tw", 1)) // only the first block is changed
	if len(stops[0].Missing()) != 1 || stops[0].Missing()[0] != "交通" || len(stops[1].Missing()) != 0 {
		t.Errorf("%v / %v", stops[0].Missing(), stops[1].Missing())
	}
}

func TestParseStops_WithoutVendorLinesIsOneStopAsBefore(t *testing.T) {
	stops := ParseStops(agreedFormat)
	if len(stops) != 1 || stops[0].Vendor != "" || stops[0].Location != ParseDescription(agreedFormat).Location {
		t.Errorf("%+v", stops)
	}
	// 地址 is only the destination inside vendor blocks.
	if st := ParseStops("地址:嘉義縣民雄鄉\n時間:1:30"); st[0].Location != "" {
		t.Errorf("plain description: 地址 must not be taken as 地點: %+v", st)
	}
}

func TestMailboxContinuationLines(t *testing.T) {
	tr := ParseDescription("信箱：a@x.org\nb@x.org、c@x.org\n\nd@x.org")
	if strings.Join(tr.Emails, ",") != "a@x.org,b@x.org,c@x.org" {
		t.Errorf("a blank line ends the continuation: %v", tr.Emails)
	}
	tr = ParseDescription("信箱：a@x.org\n時間：09:00\nb@x.org")
	if strings.Join(tr.Emails, ",") != "a@x.org" {
		t.Errorf("another label ends it: %v", tr.Emails)
	}
	tr = ParseDescription("信箱：a@x.org\n備註一些文字\nb@x.org")
	if strings.Join(tr.Emails, ",") != "a@x.org" {
		t.Errorf("a line without a mailbox ends it: %v", tr.Emails)
	}
	// the second 信箱 label (an itinerary reusing the word) must not be extended either
	tr = ParseDescription("信箱：a@x.org\n信箱：z@x.org\nq@x.org")
	if strings.Join(tr.Emails, ",") != "a@x.org" {
		t.Errorf("first label wins and only it continues: %v", tr.Emails)
	}
}

func TestEvaluate_ExtraMailboxesDoNotCauseNameNoise(t *testing.T) {
	// 1 name, 2 mailboxes (the second is a mailbox the directory doesn't have).
	tr := ParseStops(multiVendorSample)[0]
	users := []User{{SamAccountName: "zoey.xia", DisplayName: "26安環處-高雄-夏宗瑜", Mail: "zoey.xia@mail.isha.org.tw", HasDriverLicense: bp(true), HasOwnCar: bp(false)}}
	res := Evaluate(tr, users, DefaultTargetCounties)
	if len(res.Candidates) != 2 {
		t.Fatalf("one person plus the unknown mailbox, nothing else: %+v", res.Candidates)
	}
	if c := res.Candidates[0]; c.Decision != Notify || c.User.SamAccountName != "zoey.xia" {
		t.Errorf("%+v", c)
	}
	if c := res.Candidates[1]; c.Decision != Unmatched || c.Email != "isha.khh.owner@gmail.com" || c.Name != "" {
		t.Errorf("the extra mailbox is reported as not found, unlabelled: %+v", c)
	}
}
