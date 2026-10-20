package directory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const sample = `{"count":4,"users":[
 {"samAccountName":"svc_adapi","displayName":null,"mail":null,"department":null,"enabled":true,"hasDriverLicense":null,"hasOwnCar":null},
 {"samAccountName":"ytw1001","displayName":"26安環處-高雄-吳昀庭","mail":"ytw1001@mail.isha.org.tw","department":"26安環處 - 高雄","enabled":true,"hasDriverLicense":true,"hasOwnCar":false},
 {"samAccountName":"zoey.xia","displayName":"26安環處-高雄-夏宗瑜","mail":"zoey.xia@mail.isha.org.tw","department":"26安環處 - 高雄","enabled":true,"hasDriverLicense":true,"hasOwnCar":true},
 {"samAccountName":"shalafi000","displayName":"26安環處-高雄-莊明潔","mail":"shalafi000@mail.isha.org.tw","department":"26安環處 - 高雄","enabled":false,"hasDriverLicense":false,"hasOwnCar":false}]}`

func server(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

func TestListUsers_SendsKeyAndParsesTriStateValues(t *testing.T) {
	var gotKey, gotPath, gotMethod string
	base := server(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath, gotMethod = r.Header.Get("X-API-Key"), r.URL.Path, r.Method
		w.Write([]byte(sample))
	})
	users, err := NewClient().ListUsers(context.Background(), base+"/", "the-key")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "the-key" || gotPath != "/api/users" || gotMethod != "GET" {
		t.Errorf("request: key=%q path=%q method=%q", gotKey, gotPath, gotMethod)
	}
	if len(users) != 4 {
		t.Fatalf("users: %d", len(users))
	}
	svc, ytw, zoey, off := users[0], users[1], users[2], users[3]
	if svc.DisplayName != "" || svc.Mail != "" || svc.HasDriverLicense != nil || svc.HasOwnCar != nil {
		t.Errorf("nulls must stay unset, not become false/empty-but-known: %+v", svc)
	}
	if ytw.HasDriverLicense == nil || !*ytw.HasDriverLicense || ytw.HasOwnCar == nil || *ytw.HasOwnCar {
		t.Errorf("license true / car false: %+v", ytw)
	}
	if zoey.HasOwnCar == nil || !*zoey.HasOwnCar || ytw.Mail != "ytw1001@mail.isha.org.tw" || ytw.Department != "26安環處 - 高雄" {
		t.Errorf("fields: %+v %+v", zoey, ytw)
	}
	if off.Enabled || !ytw.Enabled {
		t.Errorf("enabled flag: %+v %+v", off, ytw)
	}
}

func TestListUsers_AcceptsTheFullEndpointAsTheBase(t *testing.T) {
	var gotPath string
	base := server(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"count":0,"users":[]}`))
	})
	if _, err := NewClient().ListUsers(context.Background(), base+"/api/users", "k"); err != nil || gotPath != "/api/users" {
		t.Errorf("path %q err %v", gotPath, err)
	}
}

func TestListUsers_AuthFailuresAreExplainedWithoutLeakingTheKey(t *testing.T) {
	for _, code := range []int{401, 403} {
		base := server(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		_, err := NewClient().ListUsers(context.Background(), base, "SECRET-KEY")
		if err != ErrUnauthorized || strings.Contains(err.Error(), "SECRET-KEY") {
			t.Errorf("%d: %v", code, err)
		}
	}
}

func TestListUsers_OtherFailures(t *testing.T) {
	cases := map[string]struct {
		h http.HandlerFunc
	}{
		"server error":   {func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }},
		"not json":       {func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) }},
		"no users field": {func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"count":3}`)) }},
		"count != returned": {func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"count":100,"users":[{"samAccountName":"a","enabled":true}]}`))
		}},
	}
	for name, c := range cases {
		if _, err := NewClient().ListUsers(context.Background(), server(t, c.h), "k"); err == nil {
			t.Errorf("%s: should fail", name)
		}
	}
	// An incomplete listing must be reported as such, not silently used.
	_, err := NewClient().ListUsers(context.Background(), server(t, cases["count != returned"].h), "k")
	if err != ErrIncomplete {
		t.Errorf("got %v", err)
	}
}

func TestListUsers_DoesNotFollowRedirectsSoTheKeyCannotLeave(t *testing.T) {
	var otherHits int32
	other := server(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&otherHits, 1)
		w.Write([]byte(sample))
	})
	redirector := server(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other+"/api/users", http.StatusFound)
	})
	_, err := NewClient().ListUsers(context.Background(), redirector, "SECRET-KEY")
	if err == nil {
		t.Error("a redirect must not be treated as success")
	}
	if atomic.LoadInt32(&otherHits) != 0 {
		t.Error("the redirect target received a request, so it may have received the API key")
	}
}

func TestListUsers_UnreachableHostErrorHasNoURLOrKey(t *testing.T) {
	_, err := NewClient().ListUsers(context.Background(), "http://127.0.0.1:1", "SECRET-KEY")
	if err == nil || strings.Contains(err.Error(), "SECRET-KEY") || strings.Contains(err.Error(), "127.0.0.1:1/api/users") {
		t.Errorf("got %v", err)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	good := map[string]string{
		"http://10.6.20.14:8089":               "http://10.6.20.14:8089",
		"  http://10.6.20.14:8089/ ":           "http://10.6.20.14:8089",
		"http://10.6.20.14:8089/api/users":     "http://10.6.20.14:8089",
		"http://10.6.20.14:8089/api/users/":    "http://10.6.20.14:8089",
		"https://dir.example.com/ad":           "https://dir.example.com/ad",
		"https://dir.example.com/ad/api/users": "https://dir.example.com/ad",
	}
	for in, want := range good {
		if got, err := NormalizeBaseURL(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10.6.20.14:8089", "ftp://x/y", "javascript:alert(1)", "http://", "http://user:pw@host/", "http://host/?a=b", "http://host/#frag", "file:///etc/passwd"} {
		if _, err := NormalizeBaseURL(in); err != ErrBaseURLInvalid {
			t.Errorf("%q should be rejected, got %v", in, err)
		}
	}
}
