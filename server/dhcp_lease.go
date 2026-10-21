package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// lease tracks an IP offered or bound to a client MAC.
type lease struct {
	ip      net.IP
	expires time.Time
	bound   bool
}

// leasePool allocates addresses from the configured range, one lease per MAC.
type leasePool struct {
	mu       sync.Mutex
	start    uint32
	end      uint32
	next     uint32
	byMAC    map[string]*lease
	byIP     map[string]*lease
	leaseTTL time.Duration
}

func newLeasePool(cfg DHCPConfig) (*leasePool, error) {
	s := net.ParseIP(cfg.RangeStart).To4()
	e := net.ParseIP(cfg.RangeEnd).To4()
	if s == nil || e == nil {
		return nil, fmt.Errorf("invalid dhcp range")
	}
	return &leasePool{
		start:    binaryIP(s),
		end:      binaryIP(e),
		next:     binaryIP(s),
		byMAC:    make(map[string]*lease),
		byIP:     make(map[string]*lease),
		leaseTTL: time.Duration(cfg.LeaseSeconds) * time.Second,
	}, nil
}

func binaryIP(ip net.IP) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32IP(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v)).To4()
}

func macKey(chaddr []byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", chaddr[0], chaddr[1], chaddr[2], chaddr[3], chaddr[4], chaddr[5])
}

// acquire returns an IP for the client, reusing its existing lease when valid.
// It returns an error when the pool is exhausted (no free address remains).
func (p *leasePool) acquire(chaddr []byte) (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	mac := macKey(chaddr)
	// Reuse a still-valid lease.
	if l, ok := p.byMAC[mac]; ok && now.Before(l.expires) {
		l.expires = now.Add(p.leaseTTL)
		return l.ip, nil
	}
	// Find the next free address with a bounded linear scan starting at the
	// cursor, wrapping once, so stale/expired slots are reclaimed.
	total := int(p.end - p.start + 1)
	v := p.next
	for i := 0; i < total; i++ {
		key := uint32IP(v).String()
		if held, ok := p.byIP[key]; ok && now.Before(held.expires) {
			// In use by someone else; move on.
			v++
			if v > p.end {
				v = p.start
			}
			continue
		}
		ip := uint32IP(v)
		if old, ok := p.byMAC[mac]; ok {
			delete(p.byIP, old.ip.String())
		}
		p.byMAC[mac] = &lease{ip: ip, expires: now.Add(p.leaseTTL)}
		p.byIP[key] = p.byMAC[mac]
		p.next = v + 1
		if p.next > p.end {
			p.next = p.start
		}
		return ip, nil
	}
	return nil, fmt.Errorf("dhcp pool exhausted (%d-%d)", uint32IP(p.start), uint32IP(p.end))
}

// free removes a client's lease (used on NAK/release and shutdown).
func (p *leasePool) free(chaddr []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	mac := macKey(chaddr)
	if l, ok := p.byMAC[mac]; ok {
		delete(p.byIP, l.ip.String())
		delete(p.byMAC, mac)
	}
}

// inUse reports the number of live leases (for tests and status logging).
func (p *leasePool) inUse() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	n := 0
	for _, l := range p.byMAC {
		if now.Before(l.expires) {
			n++
		}
	}
	return n
}
