package main

import (
	"net/http"
	"testing"
	"time"
)

func TestWebServerDefault(t *testing.T) {
	s := &WebServer{
		TemplatePaths: []string{"web/templates/default"},
		Frontend: map[string]string{
			"CLIENT_NAME": "AuthService",
			"THEME":       "ekf",
		},
		ProviderURL: "http://example.test",
	}
	// Start web server
	go func() {
		t.Fatal(s.Start("localhost:8082"))
	}()
	time.Sleep(3 * time.Second)
	baseURL := mustParseURL("http://localhost:8082")
	landing := baseURL.ResolveReference(mustParseURL("/site/landing"))
	afterLogout := baseURL.ResolveReference(mustParseURL("/site/after_logout"))
	image := baseURL.ResolveReference(mustParseURL("/site/assets/themes/ekf/bg.svg"))

	tests := []struct {
		name string
		url  string
	}{
		{name: "landing", url: landing.String()},
		{name: "afterLogout", url: afterLogout.String()},
		{name: "image", url: image.String()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := http.DefaultClient.Get(test.url)
			if err != nil {
				t.Fatalf("Error making http request: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("Got non-200 status code: %v", resp.StatusCode)
			}
		})
	}
}
