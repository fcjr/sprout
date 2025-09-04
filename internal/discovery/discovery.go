package discovery

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/hashicorp/mdns"
)

const serviceName = "_sprout_autodiscovery._tcp"
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

	service, err := mdns.NewMDNSService(hostname, serviceName, "", "", port, nil, []string{"sprout discovery service"})
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
	// Temporarily suppress mdns library log output
	oldOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldOutput)

	entriesCh := make(chan *mdns.ServiceEntry, 10)
	go func() {
		defer close(entriesCh)
		err := mdns.QueryContext(ctx, &mdns.QueryParam{
			Service:             serviceName,
			Domain:              "local",
			Timeout:             time.Second * 1, // Internal query timeout
			Interface:           nil,
			Entries:             entriesCh,
			WantUnicastResponse: false,
		})
		if err != nil {
			// Silently ignore network errors (common in environments without mDNS support)
			// But still wait for context timeout to provide consistent UX
			select {
			case <-ctx.Done():
			case <-time.After(time.Second * 5): // Fallback timeout
			}
		}
	}()

	// Use context timeout to control overall discovery duration
	var nodes []Node
	for {
		select {
		case entry, ok := <-entriesCh:
			if !ok {
				return nodes, nil
			}
			if len(entry.AddrV4) > 0 {
				nodes = append(nodes, Node{
					Hostname: entry.Name,
					IP:       entry.AddrV4,
					Port:     entry.Port,
				})
			}
			if len(entry.AddrV6) > 0 {
				nodes = append(nodes, Node{
					Hostname: entry.Name,
					IP:       entry.AddrV6,
					Port:     entry.Port,
				})
			}
		case <-ctx.Done():
			return nodes, nil // Don't return timeout error for discovery
		}
	}
}
