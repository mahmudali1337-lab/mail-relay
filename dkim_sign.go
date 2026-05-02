package main

import (
	"bytes"
	"crypto"
	_ "crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/emersion/go-msgauth/dkim"
)

func signDKIM(item *QueueItem) ([]byte, error) {
	at := strings.LastIndex(item.From, "@")
	if at < 0 {
		return item.Data, nil
	}
	domain := strings.ToLower(item.From[at+1:])

	var dc *DomainConf
	for i := range cfg.Domains {
		if strings.ToLower(cfg.Domains[i].Domain) == domain {
			dc = &cfg.Domains[i]
			break
		}
	}
	if dc == nil || dc.DKIMKey == "" {
		return item.Data, nil
	}

	keyData, err := os.ReadFile(dc.DKIMKey)
	if err != nil {
		return nil, fmt.Errorf("read dkim key: %w", err)
	}
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("invalid pem block")
	}
	var privKey crypto.Signer
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		privKey = k
	} else if k2, err2 := x509.ParsePKCS8PrivateKey(block.Bytes); err2 == nil {
		privKey = k2.(crypto.Signer)
	} else {
		return nil, fmt.Errorf("parse key: %w", err)
	}

	opts := &dkim.SignOptions{
		Domain:   domain,
		Selector: dc.Selector,
		Signer:   privKey,
		HeaderKeys: []string{
			"From", "To", "Subject", "Date", "Message-ID",
			"Content-Type", "MIME-Version", "Reply-To",
		},
	}

	var out bytes.Buffer
	if err := dkim.Sign(&out, bytes.NewReader(item.Data), opts); err != nil {
		return nil, fmt.Errorf("dkim sign: %w", err)
	}
	return out.Bytes(), nil
}
