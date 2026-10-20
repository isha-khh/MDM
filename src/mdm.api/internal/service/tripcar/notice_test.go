package tripcar

import (
	"strings"
	"testing"
	"time"
)

func TestRenderNotice_ContentAndOptionalRows(t *testing.T) {
	subj, body, err := RenderNotice(NoticeData{Recipient: "羅羿昇", Summary: "76工危品進廠(臺南分局)", Location: "臺南分局", Start: "2026-10-27", DaysUntil: 7})
	if err != nil {
		t.Fatal(err)
	}
	if subj != "[MDM] 出差借車提醒 — 2026-10-27 出發：76工危品進廠(臺南分局)" {
		t.Errorf("subject: %q", subj)
	}
	for _, want := range []string{"羅羿昇 您好", "2026-10-27", "7 天後", "76工危品進廠(臺南分局)", "臺南分局"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	// No 時間／交通 given: those rows are left out rather than shown empty.
	if strings.Contains(body, "<strong>時間</strong>") || strings.Contains(body, "<strong>交通</strong>") {
		t.Error("empty optional rows should be omitted")
	}
	_, body, _ = RenderNotice(NoticeData{Recipient: "x", Summary: "s", Location: "高雄", Start: "2026-10-27", Time: "08:00-17:00", Transport: "高鐵＋租車"})
	if !strings.Contains(body, "08:00-17:00") || !strings.Contains(body, "高鐵＋租車") {
		t.Error("time / transport should be shown when given")
	}
}

func TestRenderNotice_CalendarTextCannotInjectMarkupOrHeaders(t *testing.T) {
	// Anyone who can edit the calendar controls these strings.
	subj, body, err := RenderNotice(NoticeData{
		Recipient: "<img src=x onerror=alert(1)>", Summary: "<script>alert(1)</script>",
		Location: "高雄\r\nBcc: evil@example.com", Start: "2026-10-27", Transport: `"><a href="http://evil">x</a>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(subj, "\r\n") {
		t.Errorf("subject must be a single line: %q", subj)
	}
	for _, bad := range []string{"<script>", "<img src=x", `<a href="http://evil">`} {
		if strings.Contains(body, bad) {
			t.Errorf("body contains unescaped %q", bad)
		}
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("the text should still be there, escaped")
	}
}

func TestRenderNotice_SeveralStopsInOneMail(t *testing.T) {
	subj, body, err := RenderNotice(NoticeData{
		Recipient: "夏宗瑜", Summary: "[深入輔導]台灣瑞環、奇菱科技(宗瑜)", Start: "2026-10-29", DaysUntil: 7,
		Stops: []NoticeStop{
			{Vendor: "台灣瑞環", Location: "臺南市柳營區大農里環園西路二段19號", Time: "0900-1200", Transport: "開車"},
			{Vendor: "奇菱科技", Location: "臺南市新市區豐華里王甲路1號", Time: "1330-1700", Transport: "開車"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subj, "2026-10-29") || !strings.Contains(subj, "台灣瑞環") || strings.Contains(subj, "柳營區") {
		t.Errorf("subject names the trip, not its addresses: %q", subj)
	}
	for _, want := range []string{"台灣瑞環", "奇菱科技", "柳營區大農里", "新市區豐華里", "0900-1200", "1330-1700"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Count(body, "<strong>地點</strong>") != 2 {
		t.Error("one table per stop")
	}
}

func TestNoticeSubject_LongTitleIsCut(t *testing.T) {
	long := strings.Repeat("很長的標題", 30)
	if s := NoticeSubject(NoticeData{Summary: long, Start: "2026-10-29"}); len([]rune(s)) > 80 {
		t.Errorf("subject too long: %d", len([]rune(s)))
	}
}

func TestStartLabel(t *testing.T) {
	tp := time.FixedZone("tp", 8*3600)
	at := time.Date(2026, 10, 26, 16, 30, 0, 0, time.UTC) // 00:30 on the 27th in Taipei
	if got := StartLabel(at, true, tp); got != "2026-10-27" {
		t.Errorf("all-day: %q", got)
	}
	if got := StartLabel(at, false, tp); got != "2026-10-27 00:30" {
		t.Errorf("timed: %q", got)
	}
}
