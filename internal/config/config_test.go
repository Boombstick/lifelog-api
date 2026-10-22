package config

import "testing"

func TestRejectTestFlags(t *testing.T) {
	cases := []struct {
		name    string
		ginMode string
		cfg     Config
		wantErr bool
	}{
		{"release, flags off", "release", Config{}, false},
		{"release, mail capture", "release", Config{AuthMailCapture: true}, true},
		{"release, test push endpoints", "release", Config{NotifyAllowTestEndpoints: true}, true},
		{"local, both flags on", "", Config{AuthMailCapture: true, NotifyAllowTestEndpoints: true}, false},
		{"debug, both flags on", "debug", Config{AuthMailCapture: true, NotifyAllowTestEndpoints: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIN_MODE", tc.ginMode)
			if err := tc.cfg.rejectTestFlags(); (err != nil) != tc.wantErr {
				t.Fatalf("error %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewRefusesMailCaptureInRelease(t *testing.T) {
	t.Setenv("GIN_MODE", "release")
	t.Setenv("AUTH_MAIL_CAPTURE", "true")
	if _, err := New(); err == nil {
		t.Fatal("New accepted AUTH_MAIL_CAPTURE with GIN_MODE=release")
	}

	t.Setenv("AUTH_MAIL_CAPTURE", "false")
	if _, err := New(); err != nil {
		t.Fatalf("New failed with test flags off: %v", err)
	}
}
