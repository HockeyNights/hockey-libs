package grpcgateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HockeyNights/hockey-libs/requestid"
)

func TestIncomingHeaderMatcher(t *testing.T) {
	tests := map[string]struct {
		header string
		want   string
	}{
		"Authorization как есть": {"Authorization", "authorization"},
		"нижний регистр":         {"authorization", "authorization"},
		"request id как есть":    {"X-Request-Id", requestid.MetadataKey},
		"клиентский user agent":  {"User-Agent", "x-client-user-agent"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := incomingHeaderMatcher(test.header)
			if !ok {
				t.Fatalf("заголовок %s не пропускается в metadata", test.header)
			}
			if got != test.want {
				t.Fatalf("%s → %q, ожидалось %q", test.header, got, test.want)
			}
		})
	}
}

func TestOutgoingHeaderMatcher(t *testing.T) {
	got, ok := outgoingHeaderMatcher(requestid.MetadataKey)
	if !ok || got != requestid.MetadataKey {
		t.Fatalf("x-request-id → %q, ok=%v", got, ok)
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := WithCORS(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("preflight дошёл до сервиса") }),
		[]string{"http://localhost:3000"},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	request.Header.Set("Origin", "http://localhost:3000")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("статус %d", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin: %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("Allow-Headers не содержит Authorization: %q", got)
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	reached := false
	handler := WithCORS(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }),
		[]string{"http://localhost:3000"},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	request.Header.Set("Origin", "https://evil.example.com")

	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("чужому источнику выдан доступ: %q", got)
	}
	if !reached {
		t.Error("запрос не дошёл до сервиса")
	}
}
