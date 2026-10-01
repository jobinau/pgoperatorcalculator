package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutesMethodRestrictions(t *testing.T) {
	routes := routes(newLogger("ERROR"))
	for _, tc := range []struct {
		path, method, allow string
	}{
		{"/supported", http.MethodPost, http.MethodGet},
		{"/calculator", http.MethodGet, http.MethodPost},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		res := httptest.NewRecorder()
		routes.ServeHTTP(res, req)
		if res.Code != http.StatusMethodNotAllowed || res.Header().Get("Allow") != tc.allow {
			t.Fatalf("%s %s: status=%d allow=%q", tc.method, tc.path, res.Code, res.Header().Get("Allow"))
		}
	}
}

func TestCalculatorStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		contains   string
	}{
		{"ok", `{"output":"json","pgversion":{"major":17},"dbtype":"web","totalmemory":16,"totalmemoryunit":"GB","cpunum":4,"hdtype":"ssd","dbsize":"mid_ram"}`, http.StatusOK, `"type": 1001`},
		{"human", `{"output":"human","pgversion":{"major":17},"dbtype":"web","totalmemory":16,"totalmemoryunit":"GB","hdtype":"ssd","dbsize":"mid_ram"}`, http.StatusOK, "[postgres.configuration]\nmax_connections = 200\n"},
		{"invalid", `{"output":"json","pgversion":{"major":9},"dbtype":"web","totalmemory":16,"totalmemoryunit":"GB","hdtype":"ssd","dbsize":"mid_ram"}`, http.StatusBadRequest, `"type": 5001`},
		{"overload", `{"output":"json","pgversion":{"major":17},"dbtype":"web","totalmemory":1,"totalmemoryunit":"GB","connections":2000,"hdtype":"ssd","dbsize":"mid_ram"}`, http.StatusUnprocessableEntity, `"answer": {}`},
		{"malformed", `{`, http.StatusBadRequest, "malformed JSON"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/calculator", strings.NewReader(tc.body))
		res := httptest.NewRecorder()
		routes(newLogger("ERROR")).ServeHTTP(res, req)
		if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.contains) {
			t.Fatalf("%s: status=%d body=%s", tc.name, res.Code, res.Body.String())
		}
	}
}
