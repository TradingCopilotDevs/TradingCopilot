package httptransport

import "testing"

func TestHTTPLogGroupAndNoise(t *testing.T) {
	cases := []struct {
		path  string
		group string
		noise bool
	}{
		{path: "/api/meetings", group: "meetings", noise: false},
		{path: "/api/message-subscriptions", group: "messaging", noise: false},
		{path: "/api/logs", group: "logs", noise: true},
		{path: "/assets/index.js", group: "static", noise: true},
		{path: "/logs", group: "frontend", noise: true},
	}
	for _, tt := range cases {
		group := logGroupForPath(tt.path)
		if group != tt.group {
			t.Fatalf("%s group = %s, want %s", tt.path, group, tt.group)
		}
		if noise := logNoise(tt.path, group); noise != tt.noise {
			t.Fatalf("%s noise = %v, want %v", tt.path, noise, tt.noise)
		}
	}
}
