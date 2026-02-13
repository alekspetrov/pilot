package config

import (
	"strings"
	"testing"

	"github.com/alekspetrov/pilot/internal/quality"
)

// GH-1125: Test alert channel type validation
func TestConfig_Validate_AlertChannelTypes(t *testing.T) {
	tests := []struct {
		name      string
		alerts    *AlertsConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil alerts config is valid",
			alerts:  nil,
			wantErr: false,
		},
		{
			name: "empty channels is valid",
			alerts: &AlertsConfig{
				Channels: []AlertChannelConfig{},
			},
			wantErr: false,
		},
		{
			name: "valid alert channel types",
			alerts: &AlertsConfig{
				Channels: []AlertChannelConfig{
					{Type: "slack"},
					{Type: "telegram"},
					{Type: "email"},
					{Type: "webhook"},
					{Type: "pagerduty"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty channel type is valid",
			alerts: &AlertsConfig{
				Channels: []AlertChannelConfig{
					{Type: ""},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid alert channel type",
			alerts: &AlertsConfig{
				Channels: []AlertChannelConfig{
					{Type: "invalid"},
				},
			},
			wantErr:   true,
			errSubstr: "alerts.channels[0].type must be one of {slack, telegram, email, webhook, pagerduty}",
		},
		{
			name: "mixed valid and invalid types",
			alerts: &AlertsConfig{
				Channels: []AlertChannelConfig{
					{Type: "slack"},
					{Type: "unknown"},
				},
			},
			wantErr:   true,
			errSubstr: "alerts.channels[1].type must be one of {slack, telegram, email, webhook, pagerduty}",
		},
		{
			name: "case sensitive validation",
			alerts: &AlertsConfig{
				Channels: []AlertChannelConfig{
					{Type: "SLACK"},
				},
			},
			wantErr:   true,
			errSubstr: "alerts.channels[0].type must be one of {slack, telegram, email, webhook, pagerduty}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Alerts = tt.alerts

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// GH-1125: Test quality gate type validation
func TestConfig_Validate_QualityGateTypes(t *testing.T) {
	tests := []struct {
		name      string
		quality   *quality.Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil quality config is valid",
			quality: nil,
			wantErr: false,
		},
		{
			name: "empty gates is valid",
			quality: &quality.Config{
				Gates: []*quality.Gate{},
			},
			wantErr: false,
		},
		{
			name: "valid quality gate types",
			quality: &quality.Config{
				Gates: []*quality.Gate{
					{Type: quality.GateBuild},
					{Type: quality.GateTest},
					{Type: quality.GateLint},
					{Type: quality.GateCoverage},
					{Type: quality.GateSecurity},
					{Type: quality.GateTypeCheck},
					{Type: quality.GateCustom},
				},
			},
			wantErr: false,
		},
		{
			name: "empty gate type is valid",
			quality: &quality.Config{
				Gates: []*quality.Gate{
					{Type: ""},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid quality gate type",
			quality: &quality.Config{
				Gates: []*quality.Gate{
					{Type: "invalid"},
				},
			},
			wantErr:   true,
			errSubstr: "quality.gates[0].type must be one of {build, test, lint, coverage, security, typecheck, custom}",
		},
		{
			name: "mixed valid and invalid gate types",
			quality: &quality.Config{
				Gates: []*quality.Gate{
					{Type: quality.GateBuild},
					{Type: "unknown"},
				},
			},
			wantErr:   true,
			errSubstr: "quality.gates[1].type must be one of {build, test, lint, coverage, security, typecheck, custom}",
		},
		{
			name: "case sensitive validation",
			quality: &quality.Config{
				Gates: []*quality.Gate{
					{Type: "BUILD"},
				},
			},
			wantErr:   true,
			errSubstr: "quality.gates[0].type must be one of {build, test, lint, coverage, security, typecheck, custom}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Quality = tt.quality

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}