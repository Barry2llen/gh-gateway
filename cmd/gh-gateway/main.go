package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"gh-gateway/internal/localmode"
	"gh-gateway/internal/server"
	"gh-gateway/internal/version"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "gh-gateway",
		Short:         "GitHub compatibility gateway for Gitea",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown command %q", args[0])
			}
			return server.Run(cmd.Context(), server.ConfigFromEnvironment())
		},
	}
	root.AddCommand(&cobra.Command{
		Use: "serve", Short: "Run the gateway server", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return server.Run(cmd.Context(), server.ConfigFromEnvironment())
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "version", Short: "Show build version", Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "gh-gateway %s\ncommit: %s\n", version.DisplayVersion(), version.DisplayCommit())
		},
	})
	addLocalModeCommands(root)
	return root
}

func addLocalModeCommands(root *cobra.Command) {
	manager := localmode.NewDefaultManager()
	var image string
	var sshPort int
	var noSSH bool
	start := &cobra.Command{
		Use: "start <host>", Short: "Start Windows local transparent mode", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := manager.Start(cmd.Context(), localmode.StartOptions{Host: args[0], Image: image, SSHPort: sshPort, SSHProxy: !noSSH})
			if err == nil {
				fmt.Fprint(cmd.OutOrStdout(), result.String())
			}
			return err
		},
	}
	start.Flags().StringVar(&image, "image", localmode.DefaultImage(), "runtime container image")
	start.Flags().IntVar(&sshPort, "ssh-port", 22, "local and upstream SSH port")
	start.Flags().BoolVar(&noSSH, "no-ssh-proxy", false, "disable SSH TCP passthrough")
	root.AddCommand(start)
	root.AddCommand(&cobra.Command{Use: "stop", Short: "Stop local transparent mode", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := manager.Stop(cmd.Context())
		fmt.Fprint(cmd.OutOrStdout(), result)
		return err
	}})
	root.AddCommand(&cobra.Command{Use: "status", Short: "Show local transparent mode status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		report, err := manager.Status(cmd.Context())
		fmt.Fprint(cmd.OutOrStdout(), report.String())
		return err
	}})
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Diagnose local transparent mode", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		report, err := manager.Doctor(cmd.Context())
		fmt.Fprint(cmd.OutOrStdout(), report.String())
		return err
	}})
	root.AddCommand(&cobra.Command{Use: "uninstall", Short: "Remove local mode state and trusted CA", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := manager.Uninstall(cmd.Context())
		fmt.Fprint(cmd.OutOrStdout(), result)
		return err
	}})
}
