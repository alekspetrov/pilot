package tunnel

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantErr   bool
		wantProv  string
	}{
		{
			name:     "nil config uses defaults",
			config:   nil,
			wantErr:  false,
			wantProv: "cloudflare",
		},
		{
			name: "cloudflare provider",
			config: &Config{
				Provider: "cloudflare",
				Port:     9090,
			},
			wantErr:  false,
			wantProv: "cloudflare",
		},
		{
			name: "ngrok provider",
			config: &Config{
				Provider: "ngrok",
				Port:     9090,
			},
			wantErr:  false,
			wantProv: "ngrok",
		},
		{
			name: "manual mode has no provider",
			config: &Config{
				Provider: "manual",
			},
			wantErr:  false,
			wantProv: "manual",
		},
		{
			name: "empty provider defaults to manual",
			config: &Config{
				Provider: "",
			},
			wantErr:  false,
			wantProv: "manual",
		},
		{
			name: "unknown provider fails",
			config: &Config{
				Provider: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewManager(tt.config, slog.Default())

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if m == nil {
				t.Errorf("expected manager, got nil")
				return
			}

			if m.Provider() != tt.wantProv {
				t.Errorf("provider = %q, want %q", m.Provider(), tt.wantProv)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("default config should not be enabled")
	}

	if cfg.Provider != "cloudflare" {
		t.Errorf("default provider = %q, want %q", cfg.Provider, "cloudflare")
	}

	if cfg.Port != 9090 {
		t.Errorf("default port = %d, want %d", cfg.Port, 9090)
	}
}

func TestManagerStatus(t *testing.T) {
	// Test with manual mode (no provider)
	cfg := &Config{Provider: "manual"}
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.Running {
		t.Error("expected not running for manual mode")
	}

	if status.Provider != "manual" {
		t.Errorf("status provider = %q, want %q", status.Provider, "manual")
	}
}

func TestManagerURLEmpty(t *testing.T) {
	cfg := &Config{Provider: "manual"}
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if url := m.URL(); url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestManagerIsRunning(t *testing.T) {
	cfg := &Config{Provider: "manual"}
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if m.IsRunning() {
		t.Error("expected not running initially")
	}
}

func TestCheckCLI(t *testing.T) {
	// Test with a CLI that definitely exists
	path, ok := CheckCLI("ls")
	if !ok {
		t.Error("expected ls to be found")
	}
	if path == "" {
		t.Error("expected non-empty path for ls")
	}

	// Test with a CLI that doesn't exist
	_, ok = CheckCLI("definitely-not-a-real-cli-tool-xyz")
	if ok {
		t.Error("expected non-existent CLI to not be found")
	}
}

func TestCloudflareProviderName(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())
	if p.Name() != "cloudflare" {
		t.Errorf("name = %q, want %q", p.Name(), "cloudflare")
	}
}

func TestNgrokProviderName(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())
	if p.Name() != "ngrok" {
		t.Errorf("name = %q, want %q", p.Name(), "ngrok")
	}
}

func TestCloudflareIsInstalled(t *testing.T) {
	p := NewCloudflareProvider(&Config{}, slog.Default())
	// Just verify it doesn't panic - actual installation varies
	_ = p.IsInstalled()
}

func TestNgrokIsInstalled(t *testing.T) {
	p := NewNgrokProvider(&Config{}, slog.Default())
	// Just verify it doesn't panic - actual installation varies
	_ = p.IsInstalled()
}
