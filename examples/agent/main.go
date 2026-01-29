// Package main provides an example agent implementation demonstrating
// the StricklySoft Core SDK. It showcases configuration loading, agent
// lifecycle management, identity context, execution model creation,
// platform error handling, and graceful shutdown.
//
// Run with:
//
//	go run examples/agent/main.go
//
// Override configuration via environment variables:
//
//	EXAMPLE_AGENT_ID=my-agent-001 go run examples/agent/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/StricklySoft/stricklysoft-core/pkg/auth"
	"github.com/StricklySoft/stricklysoft-core/pkg/config"
	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
	"github.com/StricklySoft/stricklysoft-core/pkg/lifecycle"
	"github.com/StricklySoft/stricklysoft-core/pkg/models"
)

// AgentConfig holds configuration loaded from environment variables
// using the config loader's layered resolution model.
type AgentConfig struct {
	AgentID   string `env:"AGENT_ID" envDefault:"example-001"`
	AgentName string `env:"AGENT_NAME" envDefault:"example-agent"`
	Version   string `env:"VERSION" envDefault:"1.0.0"`
}

func main() {
	// Load configuration with EXAMPLE_ prefix.
	// e.g., EXAMPLE_AGENT_ID, EXAMPLE_AGENT_NAME, EXAMPLE_VERSION
	cfg := config.MustLoad[AgentConfig](
		config.New().WithEnvPrefix("EXAMPLE"),
	)

	// Set up structured JSON logging.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Build the agent with lifecycle hooks and capabilities.
	agent, err := lifecycle.NewBaseAgentBuilder(
		cfg.AgentID, cfg.AgentName, cfg.Version,
	).
		WithCapability(lifecycle.Capability{
			Name:        "example-processing",
			Version:     "1.0.0",
			Description: "Demonstrates SDK lifecycle management",
		}).
		WithLogger(logger).
		WithOnStart(func(ctx context.Context) error {
			logger.InfoContext(ctx, "agent startup hook: initializing resources")
			return nil
		}).
		WithOnStop(func(ctx context.Context) error {
			logger.InfoContext(ctx, "agent shutdown hook: releasing resources")
			return nil
		}).
		OnStateChange(func(old, new lifecycle.State) {
			logger.Info("state transition",
				"from", old.String(),
				"to", new.String(),
			)
		}).
		Build()
	if err != nil {
		logger.Error("failed to build agent", "error", err)
		os.Exit(1)
	}

	// Start the agent.
	ctx := context.Background()
	if err := agent.Start(ctx); err != nil {
		logger.Error("failed to start agent", "error", err)
		os.Exit(1)
	}

	// Demonstrate identity context propagation.
	identity := auth.NewBasicIdentity("user-123", auth.IdentityTypeUser,
		map[string]any{"email": "user@example.com"},
	)
	ctx = auth.ContextWithIdentity(ctx, identity)
	if id, ok := auth.IdentityFromContext(ctx); ok {
		logger.Info("identity propagated",
			"id", id.ID(),
			"type", id.Type(),
		)
	}

	// Demonstrate execution model creation.
	exec, err := models.NewExecution("user-123", "example task", "default")
	if err != nil {
		logger.Error("failed to create execution", "error", err)
	} else {
		logger.Info("execution created",
			"id", exec.ID,
			"status", exec.Status.String(),
			"terminal", exec.IsTerminal(),
		)
	}

	// Demonstrate platform error handling.
	dbErr := sserr.New(sserr.CodeUnavailableDependency,
		"database connection lost")
	if sserr.IsRetryable(dbErr) {
		logger.Warn("retryable error encountered",
			"code", dbErr.Code.String(),
			"http_status", dbErr.HTTPStatus(),
		)
	}

	// Verify agent health.
	if err := agent.Health(ctx); err != nil {
		logger.Error("health check failed", "error", err)
	} else {
		logger.Info("health check passed")
	}

	// Log agent info snapshot.
	info := agent.Info()
	logger.Info("agent info",
		"id", info.ID,
		"name", info.Name,
		"state", info.State.String(),
		"capabilities", fmt.Sprintf("%d", len(info.Capabilities)),
	)

	// Wait for shutdown signal (SIGINT or SIGTERM).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig.String())

	// Graceful shutdown.
	if err := agent.Stop(ctx); err != nil {
		logger.Error("failed to stop agent", "error", err)
		os.Exit(1)
	}

	logger.Info("agent stopped successfully")
}
