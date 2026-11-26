package config

import (
	"testing"
	"time"
)

func TestLoadNormalisesHostNames(t *testing.T) {
	t.Setenv("SYNKRA_MAIL_DATABASE_URL", "postgres://synkra@127.0.0.1:5433/dev?sslmode=disable")
	t.Setenv("SYNKRA_MAIL_HOSTNAME", "MX1.Synkra.Co.Za.")
	t.Setenv("SYNKRA_MAIL_PLATFORM_DOMAIN", "Synkra.Email.")
	t.Setenv("SYNKRA_MAIL_TRACKING_HOST", "TRK.Synkra.Email.")
	c, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Hostname != "mx1.synkra.co.za" {
		t.Fatalf("hostname = %q, want mx1.synkra.co.za", c.Hostname)
	}
	if c.PlatformDomain != "synkra.email" || c.TrackingHost != "trk.synkra.email" {
		t.Fatalf("domains not normalised: %q %q", c.PlatformDomain, c.TrackingHost)
	}
}

func TestLoginThrottleIsConfigurable(t *testing.T) {
	t.Setenv("SYNKRA_MAIL_DATABASE_URL", "postgres://synkra@127.0.0.1:5433/dev?sslmode=disable")
	t.Setenv("SYNKRA_MAIL_LOGIN_MAX_FAILS", "3")
	t.Setenv("SYNKRA_MAIL_LOGIN_WINDOW", "5m")
	t.Setenv("SYNKRA_MAIL_LOGIN_LOCKOUT", "30m")
	c, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.HTTP.LoginMaxFails != 3 || c.HTTP.LoginWindow != 5*time.Minute || c.HTTP.LoginLockoutDur != 30*time.Minute {
		t.Fatalf("login throttle not applied: %+v", c.HTTP)
	}
}
