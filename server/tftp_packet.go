package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// parseRRQ extracts the filename and mode from an RRQ packet.
// Layout: [opcode(2)][filename\0][mode\0]
func parseRRQ(pkt []byte) (filename, mode string, err error) {
	if len(pkt) < 4 {
		return "", "", errors.New("packet too short")
	}
	if binary.BigEndian.Uint16(pkt[0:2]) != opRRQ {
		return "", "", errors.New("not an RRQ")
	}
	rest := pkt[2:]
	// filename up to first NUL
	i := indexByte(rest, 0)
	if i < 0 {
		return "", "", errors.New("missing filename terminator")
	}
	filename = string(rest[:i])
	rest = rest[i+1:]
	j := indexByte(rest, 0)
	if j < 0 {
		return "", "", errors.New("missing mode terminator")
	}
	mode = string(rest[:j])
	if filename == "" {
		return "", "", errors.New("empty filename")
	}
	return filename, mode, nil
}

// buildData builds a DATA packet: [opcode(2)][block(2)][data...]
func buildData(block uint16, data []byte) []byte {
	pkt := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(pkt[0:2], opDATA)
	binary.BigEndian.PutUint16(pkt[2:4], block)
	copy(pkt[4:], data)
	return pkt
}

// parseACK parses an ACK packet and returns opcode, block, ok.
func parseACK(pkt []byte) (op, block uint16, ok bool) {
	if len(pkt) < 4 {
		return 0, 0, false
	}
	op = binary.BigEndian.Uint16(pkt[0:2])
	block = binary.BigEndian.Uint16(pkt[2:4])
	return op, block, true
}

// sendError writes an ERROR packet to the client.
func sendError(pc net.PacketConn, addr net.Addr, code uint16, msg string) error {
	pkt := make([]byte, 4+len(msg)+1)
	binary.BigEndian.PutUint16(pkt[0:2], opERROR)
	binary.BigEndian.PutUint16(pkt[2:4], code)
	copy(pkt[4:], msg)
	pkt[4+len(msg)] = 0
	_, err := pc.WriteTo(pkt, addr)
	return err
}

func indexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func sameAddr(a, b net.Addr) bool {
	return a != nil && b != nil && a.String() == b.String()
}

// RunTFTP starts a TFTP listener on addr (host:port) and serves RRQ requests
// until ctx is cancelled. It returns a bind error immediately if the address
// is unavailable (port conflict). Each request is handled concurrently with a
// per-transfer deadline.
func RunTFTP(addr, root string, log *Logger, ready chan<- struct{}) (stop func(), err error) {
	srv, err := NewTFTPServer(root, log)
	if err != nil {
		return nil, err
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("tftp listen on %s: %w", addr, err)
	}
	log.Info("tftp listening", map[string]any{"addr": addr, "root": srv.root})
	if ready != nil {
		close(ready)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			n, client, err := pc.ReadFrom(buf)
			if err != nil {
				select {
				case <-done:
					return
				default:
				}
				// Any other error ends the loop; the socket is closed on stop.
				return
			}
			req := make([]byte, n)
			copy(req, buf[:n])
			go srv.ServeConn(pc, client, req)
		}
	}()
	return func() { _ = pc.Close(); <-done }, nil
}
