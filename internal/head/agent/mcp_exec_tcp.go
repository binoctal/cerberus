package agent

import (
	"bufio"
	"context"
	"net"
	"time"
)

// sendTCP transmits over a TCP connection (original behavior).
func (e *MCPExecutor) sendTCP(ctx context.Context, ep MCPEndpoint, body []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", ep.Address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	body = append(body, '\n')
	if _, err := conn.Write(body); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	return reader.ReadBytes('\n')
}
