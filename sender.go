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
	log.Printf("[%s] resolving MX for %s", item.ID, domain)
	mxs, err := net.LookupMX(domain)
	if err != nil || len(mxs) == 0 {
		return fmt.Errorf("mx lookup %s: %w", domain, err)
	}
	for _, mx := range mxs {
		log.Printf("[%s] MX: %s (prio %d)", item.ID, mx.Host, mx.Pref)
	}

	rand.Shuffle(len(mxs), func(i, j int) { mxs[i], mxs[j] = mxs[j], mxs[i] })

	body, err := signDKIM(item)
	if err != nil {
		log.Printf("[%s] dkim sign failed: %v, sending unsigned", item.ID, err)
		body = item.Data
	} else {
		log.Printf("[%s] dkim signed ok", item.ID)
	}

	localIP := nextLocalIP()
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	if localIP != "" {
		dialer.LocalAddr = &net.TCPAddr{IP: net.ParseIP(localIP)}
		log.Printf("[%s] using local IP %s", item.ID, localIP)
	}

	for _, mx := range mxs {
		host := strings.TrimSuffix(mx.Host, ".")
		addr := net.JoinHostPort(host, "25")
		log.Printf("[%s] trying %s", item.ID, addr)
		conn, err := dialer.Dial("tcp", addr)
		if err != nil {
			log.Printf("[%s] dial %s failed: %v", item.ID, addr, err)
			continue
		}
		log.Printf("[%s] connected to %s", item.ID, addr)
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			log.Printf("[%s] smtp client error: %v", item.ID, err)
			conn.Close()
			continue
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if tlsErr := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); tlsErr != nil {
				log.Printf("[%s] STARTTLS failed (continuing): %v", item.ID, tlsErr)
			} else {
				log.Printf("[%s] TLS established with %s", item.ID, host)
			}
		}
		if err := c.Mail(item.From); err != nil {
			log.Printf("[%s] MAIL FROM error: %v", item.ID, err)
			c.Close()
			continue
		}
		for _, r := range rcpts {
			if err := c.Rcpt(r); err != nil {
				log.Printf("[%s] RCPT TO %s error: %v", item.ID, r, err)
			} else {
				log.Printf("[%s] RCPT TO %s ok", item.ID, r)
			}
		}
		wc, err := c.Data()
		if err != nil {
			log.Printf("[%s] DATA error: %v", item.ID, err)
			c.Close()
			continue
		}
		wc.Write(body)
		wc.Close()
		if err := c.Quit(); err != nil {
			log.Printf("[%s] QUIT error (ignored): %v", item.ID, err)
		}
		log.Printf("[%s] message accepted by %s", item.ID, host)
		return nil
	}

	return fmt.Errorf("all MX exhausted for %s", domain)
}
