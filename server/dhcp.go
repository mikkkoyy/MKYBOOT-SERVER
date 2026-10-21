package main

import (
	"encoding/binary"
	"errors"
	"net"
)

// DHCP message types (option 53).
const (
	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5
	dhcpNak      = 6
)

// DHCP option codes used here.
const (
	optMessageType   = 53
	optServerID      = 54
	optLeaseTime     = 51
	optSubnetMask    = 1
	optRouter        = 3
	optDNSServer     = 6
	optTFTPServer    = 66  // TFTP server name (next-server)
	optBootFileName  = 67  // boot file name
	optClientArch    = 93  // client system architecture type
	dhcpHeaderLen    = 240 // fixed BOOTP header incl. 4-byte magic cookie
	dhcpCookieOffset = 236
)

// DHCP client architecture types (option 93), Intel PXE subset.
const (
	archBIOSx86   = 0x0000 // Intel x86 BIOS
	archUEFIx86   = 0x0006 // EFI x86 (32-bit UEFI)
	archUEFIx64   = 0x0007 // EFI x86-64 (64-bit UEFI)
	archUEFIBCx64 = 0x0009 // EFI BC x86-64
)

// dhcpMessageType returns the DHCP message type from option 53, or 0.
func dhcpMessageType(opts []byte) uint8 {
	v, ok := findOption(opts, optMessageType)
	if ok && len(v) >= 1 {
		return v[0]
	}
	return 0
}

// dhcpClientArch returns option 93 as a 16-bit little-endian value, or -1.
func dhcpClientArch(opts []byte) int {
	v, ok := findOption(opts, optClientArch)
	if ok && len(v) >= 2 {
		return int(binary.LittleEndian.Uint16(v[:2]))
	}
	return -1
}

// bootFileForArch selects the boot filename for a client architecture.
// UEFI x64 gets the 64-bit ipxe.efi; legacy BIOS gets ipxe.kpxe. A 32-bit
// UEFI client is deliberately NOT offered the 64-bit loader (it is not
// bootable there) — returning "" lets the server ignore/NAK instead of
// serving a wrong-architecture file.
func bootFileForArch(cfg DHCPConfig, arch int) string {
	switch arch {
	case archUEFIx64, archUEFIBCx64:
		return cfg.BootFileUEFI
	case archUEFIx86:
		return ""
	case archBIOSx86:
		return cfg.BootFileBIOS
	default:
		return cfg.BootFileUEFI // unknown/absent: modern default
	}
}

// buildReply builds a DHCP OFFER or ACK. Layout per RFC 2131:
// op..file is 236 bytes, magic cookie 4, options follow.
func buildReply(msgType uint8, req reqInfo, offeredIP net.IP, cfg DHCPConfig, subnet *net.IPNet, bootFile string) []byte {
	pkt := make([]byte, dhcpHeaderLen)
	pkt[0] = 2 // op = BOOTREPLY
	pkt[1] = 1 // htype = Ethernet
	pkt[2] = 6 // hlen = 6
	binary.BigEndian.PutUint32(pkt[4:8], req.xid)
	binary.BigEndian.PutUint16(pkt[10:12], req.flags)
	copy(pkt[16:20], offeredIP.To4())                 // yiaddr
	if ip := net.ParseIP(cfg.NextServer); ip != nil { // siaddr
		copy(pkt[20:24], ip.To4())
	}
	copy(pkt[24:28], req.giaddr)         // giaddr echo
	copy(pkt[28:44], req.chaddr)         // chaddr (16B)
	copy(pkt[108:236], []byte(bootFile)) // file field
	pkt[dhcpCookieOffset] = 0x63         // magic cookie
	pkt[dhcpCookieOffset+1] = 0x82
	pkt[dhcpCookieOffset+2] = 0x53
	pkt[dhcpCookieOffset+3] = 0x63

	opts := make([]byte, 0, 64)
	opt := func(code byte, val []byte) {
		opts = append(opts, code, byte(len(val)))
		opts = append(opts, val...)
	}
	ip4 := func(s string) net.IP {
		if ip := net.ParseIP(s); ip != nil {
			return ip.To4()
		}
		return nil
	}
	opt(optMessageType, []byte{msgType})
	if ip := ip4(cfg.BindAddress); ip != nil { // server id
		opt(optServerID, ip)
	}
	lease := make([]byte, 4)
	binary.BigEndian.PutUint32(lease, uint32(cfg.LeaseSeconds))
	opt(optLeaseTime, lease)
	if subnet != nil {
		m := subnet.Mask
		if len(m) == 16 {
			m = m[12:]
		}
		if len(m) == 4 {
			opt(optSubnetMask, m)
		}
	}
	if ip := ip4(cfg.Gateway); ip != nil {
		opt(optRouter, ip)
	}
	if ip := ip4(cfg.DNSServer); ip != nil {
		opt(optDNSServer, ip)
	}
	if ip := ip4(cfg.NextServer); ip != nil { // option 66 next-server
		opt(optTFTPServer, ip)
	}
	if bootFile != "" {
		opt(optBootFileName, []byte(bootFile))
	}
	opts = append(opts, 255) // end
	return append(pkt, opts...)
}

// reqInfo holds the fields parsed from a client BOOTREQUEST.
type reqInfo struct {
	xid    uint32
	flags  uint16
	giaddr net.IP
	chaddr []byte // 16 bytes raw
	msgTyp uint8
	arch   int
}

// parseDHCPRequest validates and extracts a client BOOTREQUEST. It checks the
// BOOTP op and the DHCP magic cookie so stray UDP traffic is rejected early.
func parseDHCPRequest(pkt []byte) (reqInfo, error) {
	var r reqInfo
	if len(pkt) < dhcpHeaderLen {
		return r, errors.New("dhcp packet too short")
	}
	if pkt[0] != 1 { // BOOTREQUEST
		return r, errors.New("not a BOOTREQUEST")
	}
	if pkt[dhcpCookieOffset] != 0x63 || pkt[dhcpCookieOffset+1] != 0x82 ||
		pkt[dhcpCookieOffset+2] != 0x53 || pkt[dhcpCookieOffset+3] != 0x63 {
		return r, errors.New("bad DHCP magic cookie")
	}
	r.xid = binary.BigEndian.Uint32(pkt[4:8])
	r.flags = binary.BigEndian.Uint16(pkt[10:12])
	r.giaddr = append(net.IP(nil), pkt[24:28]...)
	r.chaddr = append([]byte(nil), pkt[28:28+16]...)
	opts := pkt[dhcpHeaderLen:]
	r.msgTyp = dhcpMessageType(opts)
	r.arch = dhcpClientArch(opts)
	return r, nil
}

// findOption walks a DHCP option list (RFC 1483 framing) and returns the
// value for code, honoring pad (0) and end (255) markers.
func findOption(opts []byte, code byte) ([]byte, bool) {
	i := 0
	for i < len(opts) {
		c := opts[i]
		if c == 255 { // end
			return nil, false
		}
		if c == 0 { // pad
			i++
			continue
		}
		if i+1 >= len(opts) {
			return nil, false
		}
		l := int(opts[i+1])
		if i+2+l > len(opts) {
			return nil, false
		}
		if c == code {
			return opts[i+2 : i+2+l], true
		}
		i += 2 + l
	}
	return nil, false
}
