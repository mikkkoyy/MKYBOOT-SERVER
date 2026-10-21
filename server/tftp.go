package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TFTP opcodes (RFC 1350).
const (
	opRRQ   = 1
	opWRQ   = 2
	opDATA  = 3
	opACK   = 4
	opERROR = 5
	opOACK  = 6
)

// TFTP error codes.
const (
	errNotDefined   = 0
	errFileNotFound = 1
	errAccessDenied = 2
	errIllegalOp    = 4
)

const (
	tftpBlockSize   = 512
	tftpMaxRetries  = 5
	tftpTimeout     = 3 * time.Second
	tftpMaxFileSize = 256 << 20 // 256 MiB safety cap per transfer
)

// TFTPServer serves read requests (RRQ) from a contained root directory.
type TFTPServer struct {
	root string
	log  *Logger
}

// NewTFTPServer validates and prepares a TFTP server rooted at root.
func NewTFTPServer(root string, log *Logger) (*TFTPServer, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("tftp root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("tftp root %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("tftp root %s is not a directory", abs)
	}
	return &TFTPServer{root: abs, log: log}, nil
}

// resolvePath maps a requested TFTP name to an absolute path inside the root,
// rejecting any path that would escape the root (directory traversal). A
// leading slash is tolerated (clients sometimes send "/file"), but genuine ".."
// traversal is rejected with an error rather than silently normalized.
func (s *TFTPServer) resolvePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimLeft(name, "/")
	if name == "" {
		return "", errors.New("empty filename")
	}
	// Reject NUL and control characters.
	for _, r := range name {
		if r == 0 || r < 0x20 {
			return "", errors.New("invalid characters in filename")
		}
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes tftp root")
	}
	full := filepath.Join(s.root, clean)
	// Containment check: the resolved path must stay under root.
	rootWithSep := s.root + string(os.PathSeparator)
	if full != s.root && !strings.HasPrefix(full, rootWithSep) {
		return "", errors.New("path escapes tftp root")
	}
	return full, nil
}

// ServeConn handles a single RRQ. It validates the request, replying errors
// from the listening socket, then performs the block transfer over a dedicated
// ephemeral socket (the TFTP transfer ID / TID) so ACKs from the client never
// race the listener loop and the exchange follows RFC 1350.
func (s *TFTPServer) ServeConn(pc net.PacketConn, clientAddr net.Addr, req []byte) {
	filename, mode, err := parseRRQ(req)
	if err != nil {
		s.log.Warn("tftp bad request", map[string]any{"client": clientAddr.String(), "error": err.Error()})
		_ = sendError(pc, clientAddr, errIllegalOp, err.Error())
		return
	}
	if !strings.EqualFold(mode, "octet") {
		s.log.Warn("tftp unsupported mode", map[string]any{"client": clientAddr.String(), "mode": mode})
		_ = sendError(pc, clientAddr, errIllegalOp, "only octet mode supported")
		return
	}
	path, err := s.resolvePath(filename)
	if err != nil {
		s.log.Warn("tftp rejected path", map[string]any{"client": clientAddr.String(), "file": filename, "error": err.Error()})
		_ = sendError(pc, clientAddr, errAccessDenied, "access violation")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.log.Warn("tftp file not found", map[string]any{"client": clientAddr.String(), "file": filename})
			_ = sendError(pc, clientAddr, errFileNotFound, "file not found")
		} else {
			s.log.Error("tftp open error", map[string]any{"file": filename, "error": err.Error()})
			_ = sendError(pc, clientAddr, errNotDefined, "cannot open file")
		}
		return
	}
	defer f.Close()

	info, _ := f.Stat()
	if info != nil && info.Size() > tftpMaxFileSize {
		_ = sendError(pc, clientAddr, errNotDefined, "file too large")
		return
	}

	// Dedicated transfer socket (new TID): all DATA/ACK for this transfer use
	// this port, isolated from the listening socket.
	tpc, err := net.ListenPacket("udp", ":0")
	if err != nil {
		s.log.Error("tftp transfer socket", map[string]any{"error": err.Error()})
		_ = sendError(pc, clientAddr, errNotDefined, "server error")
		return
	}
	defer tpc.Close()

	s.log.Info("tftp transfer start", map[string]any{"client": clientAddr.String(), "file": filename})
	if err := s.transfer(tpc, clientAddr, f); err != nil {
		s.log.Error("tftp transfer failed", map[string]any{"file": filename, "error": err.Error()})
		return
	}
	s.log.Info("tftp transfer complete", map[string]any{"client": clientAddr.String(), "file": filename})
}

// transfer streams the file in 512-byte blocks, waiting for an ACK per block
// and retransmitting on timeout.
func (s *TFTPServer) transfer(pc net.PacketConn, client net.Addr, f *os.File) error {
	buf := make([]byte, tftpBlockSize)
	var block uint16 = 1
	for {
		n, err := f.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		last := n < tftpBlockSize
		pkt := buildData(block, buf[:n])

		ackOK := false
		for attempt := 0; attempt < tftpMaxRetries; attempt++ {
			if err := pc.SetDeadline(time.Now().Add(tftpTimeout)); err != nil {
				return err
			}
			if _, err := pc.WriteTo(pkt, client); err != nil {
				return err
			}
			resp := make([]byte, 516)
			rn, raddr, err := pc.ReadFrom(resp)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue // retransmit
				}
				return err
			}
			if !sameAddr(raddr, client) {
				continue
			}
			op, blk, ok := parseACK(resp[:rn])
			if !ok || op != opACK || blk != block {
				// Unexpected packet; ignore and retransmit.
				continue
			}
			ackOK = true
			break
		}
		if !ackOK {
			return errors.New("transfer timed out waiting for ACK")
		}
		if last {
			return nil
		}
		block++
	}
}
