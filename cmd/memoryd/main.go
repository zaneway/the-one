package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zaneway/the-one/internal/app"
	"github.com/zaneway/the-one/internal/config"
)

const version = "v0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "memoryd: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("missing command: serve, health, status")
	}

	switch args[0] {
	case "serve":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		runtime, err := app.New(ctx, cfg, version)
		if err != nil {
			return err
		}
		defer runtime.Close()
		return runtime.Serve(ctx)
	case "health":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		return callLocalTool(context.Background(), cfg, "memory.health", map[string]any{})
	case "status":
		cfg, err := parseConfig(args[1:], true)
		if err != nil {
			return err
		}
		includeConfig := statusIncludeConfig(args[1:])
		return callLocalTool(context.Background(), cfg, "memory.status", map[string]any{
			"include_config": includeConfig,
		})
	default:
		return fmt.Errorf("unknown command %q: expected serve, health, or status", args[0])
	}
}

func parseConfig(args []string, includeStatusFlag bool) (config.Config, error) {
	fs := flag.NewFlagSet("memoryd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var overrides config.Overrides
	fs.StringVar(&overrides.ConfigPath, "config", "", "config file path")
	fs.StringVar(&overrides.DataDir, "data-dir", "", "data directory")
	fs.StringVar(&overrides.DBPath, "db-path", "", "SQLite database path")
	fs.StringVar(&overrides.MCPAddr, "mcp-addr", "", "MCP address, currently stdio")
	fs.StringVar(&overrides.LogLevel, "log-level", "", "log level: debug, info, warn, error")
	if includeStatusFlag {
		fs.Bool("include-config", false, "include non-sensitive config summary")
	}
	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}
	return config.Load(overrides)
}

func statusIncludeConfig(args []string) bool {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("config", "", "")
	fs.String("data-dir", "", "")
	fs.String("db-path", "", "")
	fs.String("mcp-addr", "", "")
	fs.String("log-level", "", "")
	includeConfig := fs.Bool("include-config", false, "")
	if err := fs.Parse(args); err != nil {
		return false
	}
	return *includeConfig
}

func callLocalTool(ctx context.Context, cfg config.Config, tool string, params map[string]any) error {
	runtime, err := app.New(ctx, cfg, version)
	if err != nil {
		return err
	}
	defer runtime.Close()
	result, toolErr := runtime.CallTool(ctx, tool, params)
	if toolErr != nil {
		return toolErr
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		slog.Error("encode tool result failed", "tool", tool, "error", err)
		return err
	}
	return nil
}
