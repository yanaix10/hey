package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	ServiceName = "_heychat._tcp"
	Domain      = "local."
	Port        = 4242
)

// Register starts broadcasting the user's presence on the network
func Register(username string) (*zeroconf.Server, error) {
	server, err := zeroconf.Register(username, ServiceName, Domain, Port, nil, nil)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Broadcasting as '%s' on port %d...\n", username, Port)
	return server, nil
}

// Lookup finds a target user's local IP address
func Lookup(targetUser string) (string, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return "", err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = resolver.Browse(ctx, ServiceName, Domain, entries)
	if err != nil {
		return "", err
	}

	for entry := range entries {
		if entry.Instance == targetUser && len(entry.AddrIPv4) > 0 {
			return entry.AddrIPv4[0].String(), nil
		}
	}

	return "", fmt.Errorf("user '%s' not found on local network", targetUser)
}