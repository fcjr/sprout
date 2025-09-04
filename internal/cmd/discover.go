package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/fcjr/sprout/internal/discovery"
	"github.com/spf13/cobra"
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover other Sprout nodes on the network",
	Long:  `Discover searches the local network for other Sprout nodes using mDNS/Bonjour.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		timeout, _ := cmd.Flags().GetDuration("timeout")
		debug, _ := cmd.Flags().GetBool("debug")

		fmt.Println("🔍 Discovering Sprout nodes...")
		fmt.Println("   Searching for Sprout nodes...")

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		nodes, err := discovery.DiscoverWithDebug(ctx, debug)
		if err != nil {
			return fmt.Errorf("failed to discover nodes: %w", err)
		}

		fmt.Printf("\n📡 Found %d Sprout node(s):\n", len(nodes))
		if len(nodes) == 0 {
			fmt.Println("   No nodes discovered on the network")
			fmt.Println("   (Make sure other Sprout nodes are running and accessible)")
			if !debug {
				fmt.Println("   Tip: Use --debug flag for more detailed network information")
			}
		} else {
			for i, node := range nodes {
				fmt.Printf("   %d. %s at %s:%d\n", i+1, node.Hostname, node.IP, node.Port)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().Duration("timeout", 5*time.Second, "Discovery timeout duration")
	discoverCmd.Flags().Bool("debug", false, "Enable debug output for troubleshooting network issues")
}
