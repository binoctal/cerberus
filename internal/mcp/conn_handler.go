// internal/mcp/conn_handler.go
package mcp

import (
	"context"
	"io"
)

// handleConn handles a single request (for testing).
func (srv *Server) handleConn(r io.Reader, w io.Writer) error {
	c := newConn(r, w)
	req, err := c.readRequest()
	if err != nil {
		return err
	}
	srv.handleRequest(context.Background(), c, req)
	return nil
}
