package main

import (
	"testing"

	"github.com/smeir/zeitspur/internal/config"
)

func TestCopilotMessageBuilderUsesDollarDisplayUnit(t *testing.T) {
	cfg := config.Config{
		App:     config.AppConfig{Language: "en"},
		Copilot: config.CopilotConfig{DisplayUnit: config.CopilotDisplayUnitDollars},
	}

	_, body := copilotMessageBuilder(cfg)(2500, 2500)
	want := "$25.00 of $25.00 US dollars consumed today."
	if body != want {
		t.Fatalf("notification body = %q, want %q", body, want)
	}
}
