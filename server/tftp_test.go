package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startTestTFTP starts a TFTP server on an ephemeral loopback port and
// returns the stop function plus the address.
func startTestTFTP(t *testing.T, root string) (addr string, stop func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv, err := NewTFTPServer(root, discardLogger())
	if err != nil {
		pc.Close()
		t.Fatalf("new tftp: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			n, client, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			req := make([]byte, n)
			copy(req, buf[:n])
			go srv.ServeConn(pc, client, req)
		}
	}()
	return pc.LocalAddr().String(), func() { pc.Close(); <-done }
}

// tftpRead performs a full RRQ/read transfer from addr for name and returns
// the assembled file bytes plus any ERROR code (0 if none). It uses an
// unconnected UDP socket so it can accept replies from the server's dedicated
// transfer socket (TID), as required by RFC 1350.
func tftpRead(t *testing.T, addr, name string) (data []byte, errCode int) {
	t.Helper()
	serverAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.WriteToUDP(buildRRQ(name, "octet"), serverAddr); err != nil {
		t.Fatalf("write rrq: %v", err)
	}
	var out bytes.Buffer
	var expectBlock uint16 = 1
	for {
		buf := make([]byte, 2048)
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		op := uint16(buf[0])<<8 | uint16(buf[1])
		if op == opERROR {
			code := uint16(buf[2])<<8 | uint16(buf[3])
			return out.Bytes(), int(code)
		}
		if op != opDATA {
			t.Fatalf("unexpected opcode %d", op)
		}
		blk := uint16(buf[2])<<8 | uint16(buf[3])
		if blk != expectBlock {
			t.Fatalf("expected block %d, got %d", expectBlock, blk)
		}
		payload := buf[4:n]
		out.Write(payload)
		// ACK back to the server's transfer socket (raddr).
		ack := []byte{0, opACK, byte(blk >> 8), byte(blk)}
		if _, err := conn.WriteToUDP(ack, raddr); err != nil {
			t.Fatalf("ack: %v", err)
		}
		if len(payload) < tftpBlockSize {
			return out.Bytes(), 0
		}
		expectBlock++
	}
}

// buildRRQ builds a read-request packet.
func buildRRQ(name, mode string) []byte {
	pkt := append([]byte{0, opRRQ}, []byte(name)...)
	pkt = append(pkt, 0)
	pkt = append(pkt, []byte(mode)...)
	pkt = append(pkt, 0)
	return pkt
}

func TestTFTPTransferLargeFile(t *testing.T) {
	root := t.TempDir()
	// 3000 bytes forces multiple 512-byte blocks.
	content := bytes.Repeat([]byte("MKYBOOT-TFTP-"), 231)[:3000]
	if err := os.WriteFile(filepath.Join(root, "ipxe.efi"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	addr, stop := startTestTFTP(t, root)
	defer stop()

	got, code := tftpRead(t, addr, "ipxe.efi")
	if code != 0 {
		t.Fatalf("unexpected error code %d", code)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %d bytes, want %d", len(got), len(content))
	}
}

func TestTFTPFileNotFound(t *testing.T) {
	root := t.TempDir()
	addr, stop := startTestTFTP(t, root)
	defer stop()

	_, code := tftpRead(t, addr, "missing.efi")
	if code != errFileNotFound {
		t.Fatalf("expected file-not-found (%d), got %d", errFileNotFound, code)
	}
}

func TestTFTPPathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	// Place a secret file OUTSIDE the root.
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	addr, stop := startTestTFTP(t, root)
	defer stop()
	for _, name := range []string{"../secret.txt", "..\\secret.txt", "/etc/passwd", "a/../../secret.txt"} {
		_, code := tftpRead(t, addr, name)
		if code == 0 {
			t.Errorf("path %q was served; traversal must be rejected", name)
		}
	}
}

func TestTFTPUnsupportedModeRejected(t *testing.T) {
	root := t.TempDir()
	addr, stop := startTestTFTP(t, root)
	defer stop()
	// netcat-style: send an RRQ with mode "netascii" directly.
	serverAddr, _ := net.ResolveUDPAddr("udp", addr)
	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	rrq := append([]byte{0, opRRQ}, []byte("x.efi")...)
	rrq = append(rrq, 0)
	rrq = append(rrq, []byte("netascii")...)
	rrq = append(rrq, 0)
	if _, err := conn.Write(rrq); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	op := uint16(buf[0])<<8 | uint16(buf[1])
	if op != opERROR {
		t.Fatalf("expected ERROR for netascii mode, got opcode %d (n=%d)", op, n)
	}
}

// TestTFTPMalformedRequests ensures stray/short/oversized packets are answered
// with an ERROR (or safely ignored) rather than crashing the server.
func TestTFTPMalformedRequests(t *testing.T) {
	root := t.TempDir()
	addr, stop := startTestTFTP(t, root)
	defer stop()
	serverAddr, _ := net.ResolveUDPAddr("udp", addr)

	cases := [][]byte{
		{},            // empty
		{0, opRRQ},    // truncated RRQ (no filename)
		{0, 99},       // unknown opcode
		{0, opRRQ, 0}, // filename terminator at start (empty name)
		append([]byte{0, opRRQ}, bytes.Repeat([]byte("A"), 1500)...), // oversized, no terminator
	}
	for i, pkt := range cases {
		conn, err := net.DialUDP("udp", nil, serverAddr)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write(pkt); err != nil {
			conn.Close()
			continue
		}
		buf := make([]byte, 2048)
		_, err = conn.Read(buf) // expect an ERROR reply (or timeout for ignored)
		if err == nil {
			op := uint16(buf[0])<<8 | uint16(buf[1])
			if op != opERROR {
				t.Errorf("case %d: expected ERROR reply, got opcode %d", i, op)
			}
		}
		conn.Close()
	}
	// Server must still be alive after malformed input.
	got, code := tftpRead(t, addr, "anything")
	_ = got
	if code != errFileNotFound {
		t.Errorf("server not responsive after malformed requests (code=%d)", code)
	}
}

// TestResolvePathContainment unit-tests the traversal guard directly.
func TestResolvePathContainment(t *testing.T) {
	root := t.TempDir()
	srv, err := NewTFTPServer(root, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	// Genuine upward traversal must be rejected with an error.
	bad := []string{
		"../secret", "..\\secret", "a/../../b",
		"sub/../../../etc/passwd", "..", "",
	}
	for _, name := range bad {
		if p, err := srv.resolvePath(name); err == nil {
			t.Errorf("resolvePath(%q) = %q, want error", name, p)
		}
	}
	// Safe names resolve under root (leading slash and literal-.. text are
	// normalized into containment, not escapes).
	good := []string{"ipxe.efi", "sub/file.kpxe", "boot/menu.ipxe", "/etc/passwd", "..%2f..%2fetc"}
	rootWithSep := root + string(os.PathSeparator)
	for _, name := range good {
		p, err := srv.resolvePath(name)
		if err != nil {
			t.Errorf("resolvePath(%q) unexpected error: %v", name, err)
			continue
		}
		if !filepath.IsAbs(p) {
			t.Errorf("resolvePath(%q) not absolute: %q", name, p)
		}
		if p != root && !strings.HasPrefix(p, rootWithSep) {
			t.Errorf("resolvePath(%q) escaped root: %q", name, p)
		}
	}
}
