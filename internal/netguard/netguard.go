// Package netguard 提供只允许连接公网地址的 HTTP 传输，防止借用户可控的网址访问服务器内网（SSRF）。
package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrPrivateAddress 表示试图连接内网或本机地址。
var ErrPrivateAddress = errors.New("出于安全考虑，不能连接内网或本机地址")

// cgnat 是运营商级 NAT 地址段（100.64.0.0/10），也视为内网。
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// DenyPrivate 在建立连接前检查解析后的 IP，拒绝回环、内网、链路本地等地址。用作 net.Dialer.Control。
func DenyPrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip) {
		return ErrPrivateAddress
	}
	return nil
}

// PublicTransport 返回只连接公网地址的传输。它不走代理：经代理时无法校验最终地址。
func PublicTransport() *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, Control: DenyPrivate}).DialContext,
		ResponseHeaderTimeout: 180 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   10,
	}
}
