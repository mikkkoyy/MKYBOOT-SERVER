package main

import (
	"errors"
	"fmt"
	"net"
)

// DHCPServer implements a minimal, isolated DHCP offerer for the lab. It is
// deliberately NOT a full DHCP server: it answers DISCOVER with OFFER and
// REQUEST with ACK/NAK, only on the configured bind address, only for the
// configured subnet/range, and it never binds a wildcard address.
type DHCPServer struct {
	cfg    DHCPConfig
	subnet *net.IPNet
	pool   *leasePool
	log    *Logger
	pc     net.PacketConn
}

// NewDHCPServer validates configuration and prepares the server without
// binding any socket, so it is safe to call in dry-run mode.
func NewDHCPServer(cfg DHCPConfig, log *Logger) (*DHCPServer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	_, ipnet, err := net.ParseCIDR(cfg.Subnet)
	if err != nil {
		return nil, fmt.Errorf("dhcp subnet: %w", err)
	}
	// Isolation guard: refuse wildcard binds even if Validate was bypassed.
	if isUnsafeBind(cfg.BindAddress) {
		return nil, errors.New("dhcp bind address is a wildcard; refusing to start on production interfaces")
	}
	pool, err := newLeasePool(cfg)
	if err != nil {
		return nil, err
	}
	return &DHCPServer{cfg: cfg, subnet: ipnet, pool: pool, log: log}, nil
}

// Addr returns the configured listen address.
func (s *DHCPServer) Addr() string {
	return net.JoinHostPort(s.cfg.BindAddress, fmt.Sprint(s.cfg.BindPort))
}

// dryRun validates configuration and reports what would be offered, without
// opening a socket. Returns a human-readable summary.
func (s *DHCPServer) dryRun() string {
	return fmt.Sprintf("DHCP dry-run OK: bind=%s subnet=%s range=%s-%s lease=%ds next-server=%s bootfile(uefi)=%s bootfile(bios)=%s",
		s.Addr(), s.cfg.Subnet, s.cfg.RangeStart, s.cfg.RangeEnd, s.cfg.LeaseSeconds,
		s.cfg.NextServer, s.cfg.BootFileUEFI, s.cfg.BootFileBIOS)
}

// Listen binds the DHCP socket. It returns an actionable error on conflict
// (e.g. another DHCP server already bound the port) so the caller can abort.
func (s *DHCPServer) Listen() error {
	if isUnsafeBind(s.cfg.BindAddress) {
		return errors.New("refusing to bind DHCP to a wildcard address")
	}
	pc, err := net.ListenPacket("udp", s.Addr())
	if err != nil {
		return fmt.Errorf("dhcp listen on %s (is another DHCP server running?): %w", s.Addr(), err)
	}
	s.pc = pc
	return nil
}

// Serve processes DHCP packets until the socket is closed. Each DISCOVER/REQUEST
// is logged with its architecture and chosen boot file for auditability.
func (s *DHCPServer) Serve() {
	buf := make([]byte, 1500)
	for {
		n, raddr, err := s.pc.ReadFrom(buf)
		if err != nil {
			return // socket closed on shutdown
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		s.handle(pkt, raddr)
	}
}

func (s *DHCPServer) handle(pkt []byte, raddr net.Addr) {
	req, err := parseDHCPRequest(pkt)
	if err != nil {
		s.log.Debug("dhcp ignored packet", map[string]any{"from": raddr.String(), "error": err.Error()})
		return
	}
	mac := macKey(req.chaddr)
	switch req.msgTyp {
	case dhcpDiscover:
		bootFile := bootFileForArch(s.cfg, req.arch)
		if bootFile == "" {
			s.log.Warn("dhcp no bootfile for arch", map[string]any{"mac": mac, "arch": req.arch})
			return // do not offer a wrong-architecture file
		}
		ip, err := s.pool.acquire(req.chaddr)
		if err != nil {
			s.log.Error("dhcp pool exhausted", map[string]any{"error": err.Error()})
			return
		}
		out := buildReply(dhcpOffer, req, ip, s.cfg, s.subnet, bootFile)
		if _, err := s.pc.WriteTo(out, raddr); err != nil {
			s.log.Error("dhcp send offer failed", map[string]any{"error": err.Error()})
			return
		}
		s.log.Info("dhcp offer", map[string]any{"mac": mac, "ip": ip.String(), "arch": req.arch, "bootfile": bootFile, "to": raddr.String()})
	case dhcpRequest:
		bootFile := bootFileForArch(s.cfg, req.arch)
		ip, err := s.pool.acquire(req.chaddr)
		if err != nil || bootFile == "" {
			out := buildReply(dhcpNak, req, net.IPv4zero, s.cfg, s.subnet, "")
			_, _ = s.pc.WriteTo(out, raddr)
			s.log.Warn("dhcp nak", map[string]any{"mac": mac, "reason": "no bootfile or pool exhausted"})
			return
		}
		out := buildReply(dhcpAck, req, ip, s.cfg, s.subnet, bootFile)
		if _, err := s.pc.WriteTo(out, raddr); err != nil {
			s.log.Error("dhcp send ack failed", map[string]any{"error": err.Error()})
			return
		}
		s.log.Info("dhcp ack", map[string]any{"mac": mac, "ip": ip.String(), "bootfile": bootFile, "to": raddr.String()})
	default:
		s.log.Debug("dhcp unhandled type", map[string]any{"type": req.msgTyp, "mac": mac})
	}
}

// Close shuts the socket down.
func (s *DHCPServer) Close() {
	if s.pc != nil {
		_ = s.pc.Close()
	}
}
