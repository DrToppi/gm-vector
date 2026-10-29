package planner

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	if len(values) > 16 {
		return nil, errors.New("too many trusted proxy networks")
	}
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, e := netip.ParsePrefix(strings.TrimSpace(value))
		if e != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
			return nil, errors.New("invalid trusted proxy CIDR")
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func (a *App) loginClientIP(r *http.Request) (string, error) {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return "", appError{400, "client_address", "Неверный адрес клиента"}
	}
	peer, e := netip.ParseAddr(host)
	if e != nil || peer.Zone() != "" {
		return "", appError{400, "client_address", "Неверный адрес клиента"}
	}
	peer = peer.Unmap()
	trusted := false
	for _, network := range a.Config.TrustedProxies {
		if network.Contains(peer) {
			trusted = true
			break
		}
	}
	if !trusted {
		return peer.String(), nil
	}
	values := r.Header.Values("X-GMA-Client-IP")
	if len(values) != 1 || len(values[0]) > 64 {
		return "", appError{400, "client_address", "Неверный адрес клиента"}
	}
	client, e := netip.ParseAddr(values[0])
	if e != nil || client.Zone() != "" || client.IsUnspecified() || client.IsMulticast() {
		return "", appError{400, "client_address", "Неверный адрес клиента"}
	}
	return client.Unmap().String(), nil
}
