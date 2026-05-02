package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/smtp"
	"strings"
	"sync/atomic"
	"time"
)

var srvIdx atomic.Int64

func nextLocalIP() string {
	if len(cfg.Servers) == 0 {
		return ""
	}
	idx := int(srvIdx.Add(1)) % len(cfg.Servers)
	return cfg.Servers[idx]
}

func sendToDomain(item *QueueItem, domain string, rcpts []string) error {
	mxs, err := net.LookupMX(domain)
	if err != nil || len(mxs) == 0 {
		return fmt.Errorf("mx lookup %s: %w", domain, err)
	}

	rand.Shuffle(len(mxs), func(i, j int) { mxs[i], mxs[j] = mxs[j], mxs[i] })

	body, err := signDKIM(item)
	if err != nil {
		log.Printf("[%s] dkim: %v, sending unsigned", item.ID, err)
		body = item.Data
	}

	localIP := nextLocalIP()
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	if localIP != "" {
		dialer.LocalAddr = &net.TCPAddr{IP: net.ParseIP(localIP)}
	}

	for _, mx := range mxs {
		host := strings.TrimSuffix(mx.Host, ".")
		conn, err := dialer.Dial("tcp", net.JoinHostPort(host, "25"))
		if err != nil {
			continue
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			continue
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		}
		if err := c.Mail(item.From); err != nil {
			c.Close()
			continue
		}
		for _, r := range rcpts {
			c.Rcpt(r)
		}
		wc, err := c.Data()
		if err != nil {
			c.Close()
			continue
		}
		wc.Write(body)
		wc.Close()
		c.Quit()
		return nil
	}

	return fmt.Errorf("all MX exhausted for %s", domain)
}
