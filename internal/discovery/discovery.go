package discovery

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const ServiceName = "_sprout._tcp"
const servicePort = 8080

type Server struct {
	server *mdns.Server
}

type Node struct {
	Hostname string
	IP       net.IP
	Port     int
}

type NewServerParams struct {
	Hostname string
	Port     int
}

func NewServer(params *NewServerParams) (*Server, error) {
	hostname := params.Hostname
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	port := params.Port
	if port == 0 {
		port = servicePort
	}

	// Get the local IP addresses to use for the mDNS service
	ips, err := getLocalIPs()
	if err != nil {
		return nil, fmt.Errorf("could not determine host IP addresses for %s: %w", hostname, err)
	}

	// Log the IPs we're advertising on
	fmt.Printf("  Advertising on IPs: ")
	for i, ip := range ips {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%s", ip.String())
	}
	fmt.Printf("\n")

	service, err := mdns.NewMDNSService(hostname, ServiceName, "", "", port, ips, []string{"sprout discovery service"})
	if err != nil {
		return nil, fmt.Errorf("failed to create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return nil, fmt.Errorf("failed to create mDNS server: %w", err)
	}

	return &Server{server: server}, nil
}

func (s *Server) Start() error {
	if s.server == nil {
		return fmt.Errorf("server not initialized")
	}
	return nil
}

func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown()
}

func Discover(ctx context.Context) ([]Node, error) {
	return DiscoverWithDebug(ctx, false)
}

func DiscoverWithDebug(ctx context.Context, debug bool) ([]Node, error) {
	entries := make(chan *mdns.ServiceEntry, 10)

	// Find suitable network interface, with debug output
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	if debug {
		fmt.Printf("Debug: Found %d network interfaces\n", len(interfaces))
	}

	var targetInterface *net.Interface
	for _, iface := range interfaces {
		if debug {
			fmt.Printf("Debug: Interface %s - Flags: %v, Up: %v, Loopback: %v\n",
				iface.Name, iface.Flags, iface.Flags&net.FlagUp != 0, iface.Flags&net.FlagLoopback != 0)
		}

		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			if debug {
				fmt.Printf("Debug: Failed to get addresses for interface %s: %v\n", iface.Name, err)
			}
			continue
		}

		// Check if this interface has IPv4 addresses
		for _, addr := range addrs {
			if debug {
				fmt.Printf("Debug: Interface %s has address: %s\n", iface.Name, addr.String())
			}
			if ipNet, ok := addr.(*net.IPNet); ok {
				if ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
					if debug {
						fmt.Printf("Debug: Selected interface %s with IPv4: %s\n", iface.Name, ipNet.IP.String())
					}
					targetInterface = &iface
					break
				}
			}
		}
		if targetInterface != nil {
			break
		}
	}

	if targetInterface == nil && debug {
		fmt.Printf("Debug: No suitable interface found, using nil (all interfaces)\n")
	}

	go func() {
		defer close(entries)

		// First try with specific interface
		err := mdns.Query(&mdns.QueryParam{
			Service:             ServiceName,
			Domain:              "local",
			Timeout:             time.Second * 2,
			Interface:           targetInterface,
			Entries:             entries,
			WantUnicastResponse: false,
		})

		if err != nil {
			if debug {
				fmt.Printf("Debug: Query with specific interface failed: %v\n", err)
				fmt.Printf("Debug: Retrying with all interfaces...\n")
			}
			// Fallback to all interfaces
			err2 := mdns.Query(&mdns.QueryParam{
				Service:             ServiceName,
				Domain:              "local",
				Timeout:             time.Second * 2,
				Interface:           nil, // Use all interfaces
				Entries:             entries,
				WantUnicastResponse: false,
			})
			if err2 != nil && debug {
				fmt.Printf("Debug: Query with all interfaces also failed: %v\n", err2)
			}
		}
	}()

	var nodes []Node
	timeout := time.After(5 * time.Second)

	for {
		select {
		case entry := <-entries:
			if entry == nil {
				return nodes, nil
			}

			// Strict validation: only accept services with our exact service name pattern
			if !strings.Contains(entry.Name, ServiceName) {
				continue // Skip entries that don't match our service
			}

			// Additional validation: check that the TXT record contains our identifier
			isValidSproutService := false
			// Check both Info (string) and InfoFields ([]string)
			if strings.Contains(entry.Info, "sprout discovery service") {
				isValidSproutService = true
			} else {
				for _, txt := range entry.InfoFields {
					if strings.Contains(txt, "sprout discovery service") {
						isValidSproutService = true
						break
					}
				}
			}

			if !isValidSproutService {
				continue // Skip services without our TXT record
			}

			// Extract hostname from service name (remove service suffix)
			hostname := entry.Name[:len(entry.Name)-len(ServiceName)]

			// Add IPv4 address if available
			if entry.AddrV4 != nil {
				nodes = append(nodes, Node{
					Hostname: hostname,
					IP:       entry.AddrV4,
					Port:     entry.Port,
				})
			}
			// Also add IPv6 if available and different
			if entry.AddrV6 != nil && (entry.AddrV4 == nil || !entry.AddrV6.Equal(entry.AddrV4)) {
				nodes = append(nodes, Node{
					Hostname: hostname,
					IP:       entry.AddrV6,
					Port:     entry.Port,
				})
			}
		case <-timeout:
			return nodes, nil
		case <-ctx.Done():
			return nodes, nil
		}
	}
}

// getLocalIPs returns the local IP addresses that can be used for mDNS
func getLocalIPs() ([]net.IP, error) {
	var ips []net.IP

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip if not a valid IP or is loopback
			if ip == nil || ip.IsLoopback() {
				continue
			}

			// Include both IPv4 and IPv6 addresses
			if ip.To4() != nil || ip.To16() != nil {
				ips = append(ips, ip)
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no suitable IP addresses found")
	}

	return ips, nil
}
