package tracker

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestHealthConfigDefaultsAndValidate(t *testing.T) {
	var nilCfg *HealthConfig
	if !nilCfg.Enabled() || nilCfg.BigFileLimit() != 1000 || nilCfg.FamilyTimeout() != 20*time.Second || len(nilCfg.Tests()) == 0 || nilCfg.Excluded() != nil || nilCfg.Validate() != nil {
		t.Fatal("nil config is not the defaults")
	}
	var cfg HealthConfig
	src := "off: true\nbig_file_lines: -1\ntest_globs: [\"**/*Spec.cs\"]\nexclude: [\"[bad\"]\ntimeout: soon\n"
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled() || cfg.BigFileLimit() != 1000 || cfg.Tests()[0] != "**/*Spec.cs" || cfg.FamilyTimeout() != 20*time.Second {
		t.Fatalf("parsed config = %+v", cfg)
	}
	got := strings.Join(cfg.Validate(), "\n")
	for _, want := range []string{"health.big_file_lines -1", "health.timeout \"soon\"", "health.exclude[0] \"[bad\""} {
		if !strings.Contains(got, want) {
			t.Errorf("Validate() missing %q:\n%s", want, got)
		}
	}
	clone := cloneHealth(&cfg)
	clone.Exclude[0] = "changed"
	if cfg.Exclude[0] != "[bad" {
		t.Fatal("clone shares the exclude slice")
	}
}

func TestHealthShrinkShotsAfter(t *testing.T) {
	var nilCfg *HealthConfig
	if d, ok := nilCfg.ShrinkShotsAfter(); !ok || d != 30*24*time.Hour {
		t.Fatalf("default = %v, %v", d, ok)
	}
	for src, want := range map[string]time.Duration{"shrink_shots_after_days: 7\n": 7 * 24 * time.Hour, "shrink_shots_after_days: 0\n": 0} {
		var cfg HealthConfig
		if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
			t.Fatal(err)
		}
		if d, ok := cfg.ShrinkShotsAfter(); d != want || ok != (want > 0) {
			t.Errorf("%q: %v, %v", src, d, ok)
		}
		clone := cloneHealth(&cfg)
		*clone.ShrinkShotsAfterDays = 99
		if *cfg.ShrinkShotsAfterDays == 99 {
			t.Fatal("clone shares shrink_shots_after_days")
		}
	}
	var neg HealthConfig
	yaml.Unmarshal([]byte("shrink_shots_after_days: -2\n"), &neg)
	if _, ok := neg.ShrinkShotsAfter(); ok {
		t.Error("negative days turned shrinking on")
	}
	if got := strings.Join(neg.Validate(), "\n"); !strings.Contains(got, "health.shrink_shots_after_days -2 is negative") {
		t.Errorf("Validate() = %q", got)
	}
}

func TestHealthLimits(t *testing.T) {
	var nilCfg *HealthConfig
	if v, ok := nilCfg.Limit("waiting_share"); !ok || v != 0.25 {
		t.Fatalf("default waiting_share = %v, %v", v, ok)
	}
	if _, ok := nilCfg.Limit("nope"); ok {
		t.Fatal("unknown limit reported as set")
	}
	var cfg HealthConfig
	if err := yaml.Unmarshal([]byte("limits:\n  waiting_share: 0.4\n  big_files: 0\n  ci_red: -1\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if v, ok := cfg.Limit("waiting_share"); !ok || v != 0.4 {
		t.Fatalf("waiting_share = %v, %v", v, ok)
	}
	if _, ok := cfg.Limit("big_files"); ok {
		t.Fatal("big_files: 0 should turn the limit off")
	}
	if got := strings.Join(cfg.Validate(), "\n"); !strings.Contains(got, "health.limits.ci_red -1") {
		t.Fatalf("Validate() = %q", got)
	}
	clone := cloneHealth(&cfg)
	clone.Limits["waiting_share"] = 1
	if cfg.Limits["waiting_share"] != 0.4 {
		t.Fatal("clone shares the limits map")
	}
}
