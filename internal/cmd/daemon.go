package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fcjr/sprout/internal/discovery"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run Sprout discovery daemon",
	Long: `Run Sprout as a daemon that announces this node on the network via mDNS.
This allows other Sprout nodes to discover this machine using the 'sprout discover' command.

This daemon is typically run as a systemd service on NixOS installations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		hostname, _ := cmd.Flags().GetString("hostname")
		port, _ := cmd.Flags().GetInt("port")
		quiet, _ := cmd.Flags().GetBool("quiet")

		if !quiet {
			fmt.Printf("Starting Sprout daemon...\n")
			fmt.Printf("  Hostname: %s\n", hostname)
			fmt.Printf("  Port: %d\n", port)
		}

		// Create discovery server
		server, err := discovery.NewServer(&discovery.NewServerParams{
			Hostname: hostname,
			Port:     port,
		})
		if err != nil {
			return fmt.Errorf("failed to create discovery server: %w", err)
		}

		// Start the server
		if err := server.Start(); err != nil {
			return fmt.Errorf("failed to start discovery server: %w", err)
		}

		if !quiet {
			fmt.Printf("✓ Sprout daemon started successfully\n")
			fmt.Printf("  Service: _sprout_autodiscovery._tcp\n")
			fmt.Printf("  Announcing on mDNS/Bonjour\n\n")
			fmt.Printf("Press Ctrl+C to stop the daemon\n")
		}

		// Set up signal handling for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		// Also set up a ticker for periodic status (if not quiet)
		var ticker *time.Ticker
		if !quiet {
			ticker = time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
		}

		// Keep the daemon running
		for {
			select {
			case <-sigChan:
				if !quiet {
					fmt.Printf("\nReceived shutdown signal, stopping daemon...\n")
				}
				if err := server.Stop(); err != nil {
					log.Printf("Error stopping server: %v\n", err)
				}
				if !quiet {
					fmt.Printf("Sprout daemon stopped\n")
				}
				return nil
			case <-func() <-chan time.Time {
				if ticker != nil {
					return ticker.C
				}
				return make(<-chan time.Time) // Never fires if quiet mode
			}():
				// Periodic status update (only if not quiet)
				fmt.Printf("[%s] Sprout daemon is running (hostname: %s, port: %d)\n",
					time.Now().Format("2006-01-02 15:04:05"), hostname, port)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)

	// Get default hostname
	defaultHostname, err := os.Hostname()
	if err != nil {
		defaultHostname = "sprout-node"
	}

	daemonCmd.Flags().String("hostname", defaultHostname, "Hostname to announce on the network")
	daemonCmd.Flags().Int("port", 8080, "Port number to announce")
	daemonCmd.Flags().Bool("quiet", false, "Run in quiet mode (minimal output, suitable for systemd)")
}