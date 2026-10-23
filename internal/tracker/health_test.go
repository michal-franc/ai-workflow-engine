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
