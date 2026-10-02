package app

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestPublicDialFallsBackBeforeTheRequestDeadline(t *testing.T) {
	ips := []net.IPAddr{{IP: net.ParseIP("2606:4700:4700::1111")}, {IP: net.ParseIP("8.8.8.8")}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	conn, err := dialPublicAddresses(ctx, "tcp", "443", ips, func(attempt context.Context, network, address string) (net.Conn, error) {
		calls++
		if network != "tcp" {
			t.Fatal("changed requested network")
		}
		if address == "[2606:4700:4700::1111]:443" {
			<-attempt.Done() // Simulate a blackhole without sending network packets.
			return nil, attempt.Err()
		}
		if address != "8.8.8.8:443" || attempt.Err() != nil {
			t.Fatal("fallback lost its validated destination or usable time budget")
		}
		a, b := net.Pipe()
		b.Close()
		return a, nil
	})
	if conn != nil {
		conn.Close()
	}
	if err != nil || calls != 2 {
		t.Fatalf("did not reach working fallback: attempts=%d error=%v", calls, err)
	}
}

func TestPublicDialCancellationStopsRemainingAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ips := []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("1.1.1.1")}}
	calls := 0
	conn, err := dialPublicAddresses(ctx, "tcp", "443", ips, func(attempt context.Context, _, _ string) (net.Conn, error) {
		calls++
		cancel()
		<-attempt.Done()
		return nil, attempt.Err()
	})
	if conn != nil || err == nil || calls != 1 {
		t.Fatalf("continued dialing after cancellation: attempts=%d error=%v", calls, err)
	}
}
