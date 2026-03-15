package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ichinya/quiverkeep-core/internal/api/server"
	"github.com/ichinya/quiverkeep-core/internal/cli/doctor"
	"github.com/ichinya/quiverkeep-core/internal/cli/httpclient"
	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage"
	"github.com/ichinya/quiverkeep-core/internal/version"
)

type Options struct {
	ConfigPath string
	URL        string
	Bind       string
	Port       int
	Token      string
	LogLevel   string
	AsJSON     bool
}

func Execute(ctx context.Context) error {
	opts := &Options{}
	root := buildRootCommand(ctx, opts)
	return root.Execute()
}

func buildRootCommand(ctx context.Context, opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quiverkeep",
		Short: "QuiverKeep core server and thin CLI client",
	}

	cmd.PersistentFlags().StringVar(&opts.ConfigPath, "config-path", "", "Path to config.json")
	cmd.PersistentFlags().StringVar(&opts.URL, "url", "", "Core base URL for thin-client commands")
	cmd.PersistentFlags().StringVar(&opts.Bind, "bind", "", "Bind address for serve")
	cmd.PersistentFlags().IntVar(&opts.Port, "port", 0, "Port for serve")
	cmd.PersistentFlags().StringVar(&opts.Token, "token", "", "Bearer token override")
	cmd.PersistentFlags().StringVar(&opts.LogLevel, "log-level", "", "Log level override")
	cmd.PersistentFlags().BoolVar(&opts.AsJSON, "json", false, "Output as JSON for read commands")

	cmd.AddCommand(buildServeCommand(ctx, opts))
	cmd.AddCommand(buildStatusCommand(ctx, opts))
	cmd.AddCommand(buildUsageCommand(ctx, opts))
	cmd.AddCommand(buildLimitsCommand(ctx, opts))
	cmd.AddCommand(buildProxyCommand(ctx, opts))
	cmd.AddCommand(buildConfigCommand(ctx, opts))
	cmd.AddCommand(buildDoctorCommand(ctx, opts))
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println(version.BuildVersion)
			return nil
		},
	})

	return cmd
}

func buildServeCommand(ctx context.Context, opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run core HTTP API server",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger, cfg, meta, err := prepareRuntime(opts)
			if err != nil {
				return err
			}

			logger.Info("serve command start", "component", "cli", "operation", "serve", "config_path", meta.Path, "bind", cfg.Core.Bind, "port", cfg.Core.Port)

			store, err := storage.New(cfg, meta, logger)
			if err != nil {
				return err
			}
			defer store.Close()

			srv := server.New(cfg, logger, store)
			runCtx, cancel := signalContext(ctx)
			defer cancel()

			return srv.Run(runCtx)
		},
	}
}

func buildStatusCommand(ctx context.Context, opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Get core status via HTTP",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runReadCommand(ctx, opts, "/api/v1/status", nil)
		},
	}
}

func buildUsageCommand(ctx context.Context, opts *Options) *cobra.Command {
	var service string
	var from string
	var to string

	command := &cobra.Command{
		Use:   "usage",
		Short: "Get usage summary via HTTP",
		RunE: func(_ *cobra.Command, _ []string) error {
			query := url.Values{}
			if strings.TrimSpace(service) != "" {
				query.Set("service", strings.TrimSpace(service))
			}
			if strings.TrimSpace(from) != "" {
				query.Set("from", strings.TrimSpace(from))
			}
			if strings.TrimSpace(to) != "" {
				query.Set("to", strings.TrimSpace(to))
			}
			return runReadCommand(ctx, opts, "/api/v1/usage", query)
		},
	}
	command.Flags().StringVar(&service, "service", "", "Service filter")
	command.Flags().StringVar(&from, "from", "", "RFC3339 lower boundary")
	command.Flags().StringVar(&to, "to", "", "RFC3339 upper boundary")
	return command
}

func buildLimitsCommand(ctx context.Context, opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "limits",
		Short: "Get limits summary via HTTP",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runReadCommand(ctx, opts, "/api/v1/limits", nil)
		},
	}
}

func buildProxyCommand(ctx context.Context, opts *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "proxy",
		Short: "Proxy diagnostics and operations via HTTP",
	}

	command.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Get proxy diagnostics via HTTP",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runReadCommand(ctx, opts, "/api/v1/proxy/status", nil)
		},
	})

	return command
}

func buildConfigCommand(ctx context.Context, opts *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration",
	}

	command.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print sanitized config",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger, cfg, _, err := prepareRuntime(opts)
			if err != nil {
				return err
			}
			logger.Info("config show", "component", "cli", "operation", "config_show")
			output(cfg.Sanitized(), opts.AsJSON)
			return nil
		},
	})

	command.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print active config path",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger, _, meta, err := prepareRuntime(opts)
			if err != nil {
				return err
			}
			logger.Info("config path", "component", "cli", "operation", "config_path", "path", meta.Path)
			fmt.Println(meta.Path)
			return nil
		},
	})

	return command
}

func buildDoctorCommand(ctx context.Context, opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger, cfg, _, err := prepareRuntime(opts)
			if err != nil {
				return err
			}

			token := ""
			if cfg.Core.Token != nil {
				token = *cfg.Core.Token
			}
			client := httpclient.New(resolveURL(cfg, opts), token, logger)
			report, err := doctor.Run(ctx, client, logger)
			output(report, opts.AsJSON)
			return err
		},
	}
}

func runReadCommand(ctx context.Context, opts *Options, path string, query url.Values) error {
	logger, cfg, _, err := prepareRuntime(opts)
	if err != nil {
		return err
	}

	token := ""
	if cfg.Core.Token != nil {
		token = *cfg.Core.Token
	}

	client := httpclient.New(resolveURL(cfg, opts), token, logger)
	var payload map[string]any
	if err := client.GetJSON(ctx, path, query, &payload); err != nil {
		return err
	}

	output(payload, opts.AsJSON)
	return nil
}

func prepareRuntime(opts *Options) (*logging.Logger, config.Config, config.Metadata, error) {
	bootstrap, err := logging.New(logging.Config{
		Level: chooseLogLevel(opts.LogLevel),
	})
	if err != nil {
		return nil, config.Config{}, config.Metadata{}, err
	}

	cfg, meta, err := config.Load(config.LoadOptions{
		ConfigPath: opts.ConfigPath,
		URL:        opts.URL,
		Bind:       opts.Bind,
		Port:       opts.Port,
		Token:      opts.Token,
		LogLevel:   opts.LogLevel,
	}, bootstrap)
	if err != nil {
		return nil, config.Config{}, config.Metadata{}, err
	}

	logger, err := logging.New(logging.Config{
		Level: cfg.Logging.Level,
		Path:  cfg.Logging.Path,
	})
	if err != nil {
		return nil, config.Config{}, config.Metadata{}, err
	}

	return logger, cfg, meta, nil
}

func resolveURL(cfg config.Config, opts *Options) string {
	if strings.TrimSpace(opts.URL) != "" {
		return strings.TrimSpace(opts.URL)
	}
	return cfg.EffectiveURL()
}

func chooseLogLevel(flagLevel string) string {
	if strings.TrimSpace(flagLevel) != "" {
		return strings.TrimSpace(flagLevel)
	}
	if env := strings.TrimSpace(os.Getenv("QUIVERKEEP_LOG_LEVEL")); env != "" {
		return env
	}
	return "info"
}

func output(value any, asJSON bool) {
	if asJSON {
		payload, _ := json.MarshalIndent(value, "", "  ")
		fmt.Println(string(payload))
		return
	}
	payload, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(payload))
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	return ctx, cancel
}
