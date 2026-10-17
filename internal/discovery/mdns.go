package discovery

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	ServiceName = "_heychat._tcp"
	Domain      = "local."
	Port        = 4242
	DefaultRelay = "104.214.168.159:4244"
)

func GetRelayAddr() string {
	if addr := os.Getenv("HEY_RELAY"); addr != "" {
		return addr
	}
	return DefaultRelay
}

func Register(username string) (*zeroconf.Server, error) {
	server, err := zeroconf.Register(username, ServiceName, Domain, Port, nil, nil)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Broadcasting locally as '%s' on port %d...\n", username, Port)
	return server, nil
}

func Lookup(targetUser string) (string, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return "", err
	}

	entries := make(chan *zeroconf.ServiceEntry)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := resolver.Browse(ctx, ServiceName, Domain, entries); err != nil {
		return "", err
	}

	for entry := range entries {
		if entry.Instance == targetUser && len(entry.AddrIPv4) > 0 {
			return entry.AddrIPv4[0].String(), nil
		}
	}

	return "", fmt.Errorf("user '%s' not found on local Wi-Fi", targetUser)
}