package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/alekspetrov/pilot/internal/banner"
	"github.com/alekspetrov/pilot/internal/config"
	"github.com/alekspetrov/pilot/internal/pilot"
)

var (
	version   = "0.1.0"
	buildTime = "unknown"
	cfgFile   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "pilot",
		Short: "AI that ships your tickets",
		Long:  `Pilot is an autonomous AI development pipeline that receives tickets, implements features, and creates PRs.`,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.pilot/config.yaml)")

	rootCmd.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newInitCmd(),
		newVersionCmd(),
		newTestCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newStartCmd() *cobra.Command {
	var daemon bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Pilot daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			// Create and start Pilot
			p, err := pilot.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to create Pilot: %w", err)
			}

			if err := p.Start(); err != nil {
				return fmt.Errorf("failed to start Pilot: %w", err)
			}

			// Show startup banner
			gateway := fmt.Sprintf("http://%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
			banner.StartupBanner(version, gateway)

			// Wait for shutdown signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			<-sigCh
			fmt.Println("\n🛑 Shutting down...")

			return p.Stop()
		},
	}

	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run in background (daemon mode)")

	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Pilot daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Send shutdown signal to running daemon
			fmt.Println("🛑 Stopping Pilot daemon...")
			fmt.Println("   (Not implemented - use Ctrl+C to stop)")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Pilot status and running tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config to get gateway address
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			fmt.Println("📊 Pilot Status")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Gateway: http://%s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
			fmt.Println()

			// Check adapters
			fmt.Println("Adapters:")
			if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled {
				fmt.Println("  ✓ Linear (enabled)")
			} else {
				fmt.Println("  ○ Linear (disabled)")
			}
			if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
				fmt.Println("  ✓ Slack (enabled)")
			} else {
				fmt.Println("  ○ Slack (disabled)")
			}
			fmt.Println()

			// List projects
			fmt.Println("Projects:")
			if len(cfg.Projects) == 0 {
				fmt.Println("  (none configured)")
			} else {
				for _, proj := range cfg.Projects {
					nav := ""
					if proj.Navigator {
						nav = " [Navigator]"
					}
					fmt.Printf("  • %s: %s%s\n", proj.Name, proj.Path, nav)
				}
			}

			return nil
		},
	}
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize Pilot configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := config.DefaultConfigPath()

			// Check if config already exists
			if _, err := os.Stat(configPath); err == nil {
				fmt.Printf("Config already exists at %s\n", configPath)
				fmt.Println("Edit it manually or delete to reinitialize.")
				return nil
			}

			// Create default config
			cfg := config.DefaultConfig()

			// Save config
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			// Show banner
			banner.PrintWithVersion(version)

			fmt.Println("   ✅ Initialized!")
			fmt.Printf("   Config: %s\n", configPath)
			fmt.Println()
			fmt.Println("   Next steps:")
			fmt.Println("   1. Edit config with your API keys")
			fmt.Println("   2. Add your projects")
			fmt.Println("   3. Run 'pilot start'")

			return nil
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show Pilot version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Pilot v%s\n", version)
			if buildTime != "unknown" {
				fmt.Printf("Built: %s\n", buildTime)
			}
		},
	}
}

func newTestCmd() *cobra.Command {
	var projectPath string
	var taskTitle string
	var taskDesc string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test Pilot with a sample task (no Linear required)",
		RunE: func(cmd *cobra.Command, args []string) error {
			banner.Print()

			if projectPath == "" {
				cwd, _ := os.Getwd()
				projectPath = cwd
			}

			fmt.Println("🧪 Running test task...")
			fmt.Printf("   Project: %s\n", projectPath)
			fmt.Printf("   Task: %s\n", taskTitle)
			fmt.Println()

			// Simulate what would happen
			fmt.Println("📋 Task Document Preview:")
			fmt.Println("─────────────────────────────────────")
			fmt.Printf("# TASK-TEST: %s\n\n", taskTitle)
			fmt.Printf("## Description\n%s\n\n", taskDesc)
			fmt.Println("## Generated by Pilot orchestrator")
			fmt.Println("─────────────────────────────────────")
			fmt.Println()

			fmt.Println("✅ Test complete! In production, Pilot would:")
			fmt.Println("   1. Create task doc in .agent/tasks/")
			fmt.Println("   2. Spawn Claude Code with the task")
			fmt.Println("   3. Create branch, implement, commit")
			fmt.Println("   4. Open PR and notify Slack")

			return nil
		},
	}

	cmd.Flags().StringVarP(&projectPath, "project", "p", "", "Project path (default: current directory)")
	cmd.Flags().StringVarP(&taskTitle, "title", "t", "Add hello endpoint", "Task title")
	cmd.Flags().StringVarP(&taskDesc, "desc", "d", "Create a /hello endpoint that returns Hello World", "Task description")

	return cmd
}
