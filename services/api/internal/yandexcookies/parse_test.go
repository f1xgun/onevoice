package yandexcookies

import (
	"encoding/json"
	"strings"
	"testing"
)

const validSessionID = "3:1701234567.5.0.1701234567890:Vqx9aBCDeFgHiJkLmNoPq:1.1|abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH"

func TestParse_JSONArray(t *testing.T) {
	input := `[
		{"name":"Session_id","value":"` + validSessionID + `","domain":".yandex.ru","path":"/"},
		{"name":"sessionid2","value":"3:abc...","domain":".yandex.ru","path":"/"}
	]`
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Format != "json" {
		t.Errorf("Format = %q, want json", got.Format)
	}
	if len(got.Cookies) != 2 {
		t.Fatalf("len = %d, want 2", len(got.Cookies))
	}
	if got.Cookies[0].Name != "Session_id" {
		t.Errorf("first cookie name = %q", got.Cookies[0].Name)
	}
}

func TestParse_JSONArray_DefaultsDomainAndPath(t *testing.T) {
	input := `[{"name":"Session_id","value":"` + validSessionID + `"}]`
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Cookies[0].Domain != ".yandex.ru" {
		t.Errorf("domain default = %q", got.Cookies[0].Domain)
	}
	if got.Cookies[0].Path != "/" {
		t.Errorf("path default = %q", got.Cookies[0].Path)
	}
}

func TestParse_JSONArray_MissingSessionID(t *testing.T) {
	input := `[{"name":"yandex_login","value":"someuser"}]`
	_, err := Parse(input)
	if err != ErrNoSessionID {
		t.Errorf("err = %v, want ErrNoSessionID", err)
	}
}

func TestParse_CookieHeader(t *testing.T) {
	input := "Session_id=" + validSessionID + "; sessionid2=3:abc; yandex_login=someuser"
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Format != "cookie_header" {
		t.Errorf("Format = %q, want cookie_header", got.Format)
	}
	if len(got.Cookies) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Cookies))
	}
}

func TestParse_CookieHeader_WithCookiePrefix(t *testing.T) {
	input := "Cookie: Session_id=" + validSessionID + "; sessionid2=3:abc"
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Format != "cookie_header" {
		t.Errorf("Format = %q, want cookie_header", got.Format)
	}
	if got.Cookies[0].Name != "Session_id" {
		t.Errorf("first cookie name = %q", got.Cookies[0].Name)
	}
}

func TestParse_SingleCookieHeaderPair(t *testing.T) {
	// "Session_id=..." with no other cookies — routes through the
	// cookie-header parser as a single-element header.
	input := "Session_id=" + validSessionID
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Format != "cookie_header" {
		t.Errorf("Format = %q, want cookie_header", got.Format)
	}
	if len(got.Cookies) != 1 {
		t.Fatalf("len = %d, want 1", len(got.Cookies))
	}
	if got.Cookies[0].Name != "Session_id" {
		t.Errorf("name = %q", got.Cookies[0].Name)
	}
}

func TestParse_BareSessionIDValue(t *testing.T) {
	got, err := Parse(validSessionID)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Format != "session_id_value" {
		t.Errorf("Format = %q, want session_id_value", got.Format)
	}
	if got.Cookies[0].Value != validSessionID {
		t.Errorf("value mismatch")
	}
}

func TestParse_Empty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t  \n"} {
		if _, err := Parse(in); err != ErrEmpty {
			t.Errorf("Parse(%q) err = %v, want ErrEmpty", in, err)
		}
	}
}

func TestParse_GarbageInput(t *testing.T) {
	cases := []string{
		"hello world",
		"Session_id",
		"Session_id=short",
		"Session_id=ABC123notavalidsessionvalue",
	}
	for _, in := range cases {
		_, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q) returned nil, want error", in)
		}
	}
}

func TestParse_JSONArray_InvalidShape(t *testing.T) {
	_, err := Parse("[{not json")
	if err == nil {
		t.Errorf("Parse invalid JSON returned nil")
	}
}

func TestParse_JSONArray_SessionIDInvalidValue(t *testing.T) {
	input := `[{"name":"Session_id","value":"shortbad"}]`
	_, err := Parse(input)
	if err != ErrSessionIDInvalid {
		t.Errorf("err = %v, want ErrSessionIDInvalid", err)
	}
}

func TestParsed_JSONIsValid(t *testing.T) {
	p, _ := Parse("Session_id=" + validSessionID)
	out := p.JSON()
	var roundTrip []map[string]any
	if err := json.Unmarshal([]byte(out), &roundTrip); err != nil {
		t.Fatalf("JSON output not parseable: %v\noutput: %s", err, out)
	}
	if len(roundTrip) != 1 {
		t.Fatalf("len = %d, want 1", len(roundTrip))
	}
	if roundTrip[0]["name"] != "Session_id" {
		t.Errorf("name = %v", roundTrip[0]["name"])
	}
}

func TestParsed_JSONShapeMatchesInjectCookies(t *testing.T) {
	// The agent's injectCookies (services/agent-yandex-business/internal/yandex/pool.go)
	// reads name, value, domain, path from each map. Make sure our JSON
	// output carries those exact lowercase keys.
	p, _ := Parse("Session_id=" + validSessionID)
	out := p.JSON()
	for _, key := range []string{`"name"`, `"value"`, `"domain"`, `"path"`} {
		if !strings.Contains(out, key) {
			t.Errorf("output missing key %s\nout: %s", key, out)
		}
	}
}
