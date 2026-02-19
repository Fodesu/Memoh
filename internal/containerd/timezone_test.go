package containerd

import (
	"os"
	"testing"
)

func TestTimezoneSpecOpts_WithTZ(t *testing.T) {
	t.Setenv("TZ", "Asia/Shanghai")
	opts := TimezoneSpecOpts()
	if _, err := os.Stat("/etc/localtime"); err == nil {
		if len(opts) < 1 {
			t.Fatal("expected at least mount opt when /etc/localtime exists")
		}
	}
	found := false
	for range opts {
		found = true
	}
	if !found {
		t.Fatal("expected at least one spec opt when TZ is set")
	}
}

func TestTimezoneSpecOpts_WithoutTZ(t *testing.T) {
	t.Setenv("TZ", "")
	opts := TimezoneSpecOpts()
	for _, opt := range opts {
		if opt == nil {
			t.Fatal("unexpected nil spec opt")
		}
	}
	if _, err := os.Stat("/etc/localtime"); err != nil && len(opts) != 0 {
		t.Fatalf("expected no opts when /etc/localtime absent and TZ empty, got %d", len(opts))
	}
}
