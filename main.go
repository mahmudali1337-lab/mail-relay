package main

import (
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen   string       `yaml:"listen"`
	Hostname string       `yaml:"hostname"`
	Password string       `yaml:"password"`
	QueueDir string       `yaml:"queue_dir"`
	Workers  int          `yaml:"workers"`
	RetryMax int          `yaml:"retry_max"`
	Servers  []string     `yaml:"servers"`
	Domains  []DomainConf `yaml:"domains"`
	TLSCert  string       `yaml:"tls_cert"`
	TLSKey   string       `yaml:"tls_key"`
}

type DomainConf struct {
	Domain   string `yaml:"domain"`
	DKIMKey  string `yaml:"dkim_key"`
	Selector string `yaml:"selector"`
}

var cfg Config

type Backend struct{}

func (b *Backend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &Session{}, nil
}

type Session struct {
	from string
	to   []string
	buf  bytes.Buffer
}

func (s *Session) AuthPlain(_, password string) error {
	if cfg.Password != "" && password != cfg.Password {
		return gosmtp.ErrAuthFailed
	}
	return nil
}

func (s *Session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *Session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	if _, err := io.Copy(&s.buf, r); err != nil {
		return err
	}
	return enqueue(s.from, s.to, s.buf.Bytes())
}

func (s *Session) Reset() {
	s.from = ""
	s.to = nil
	s.buf.Reset()
}

func (s *Session) Logout() error {
	return nil
}

func main() {
	path := "config.yaml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read config: %v", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("parse config: %v", err)
	}

	if cfg.Listen == "" {
		cfg.Listen = ":587"
	}
	if cfg.Hostname == "" {
		cfg.Hostname = "localhost"
	}
	if cfg.QueueDir == "" {
		cfg.QueueDir = "/var/spool/mailrelay"
	}
	if cfg.Workers == 0 {
		cfg.Workers = 5
	}
	if cfg.RetryMax == 0 {
		cfg.RetryMax = 10
	}

	if err := os.MkdirAll(cfg.QueueDir, 0700); err != nil {
		log.Fatalf("queue dir: %v", err)
	}
	if err := os.MkdirAll(cfg.QueueDir+"/failed", 0700); err != nil {
		log.Fatalf("failed dir: %v", err)
	}

	for i := 0; i < cfg.Workers; i++ {
		go startWorker()
	}

	srv := gosmtp.NewServer(&Backend{})
	srv.Addr = cfg.Listen
	srv.Domain = cfg.Hostname
	srv.ReadTimeout = 30 * time.Second
	srv.WriteTimeout = 30 * time.Second
	srv.MaxMessageBytes = 25 * 1024 * 1024
	srv.MaxRecipients = 100
	srv.AllowInsecureAuth = true

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, tlsErr := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if tlsErr != nil {
			log.Fatalf("tls: %v", tlsErr)
		}
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	go func() {
		log.Printf("SMTP relay listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("smtp: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	srv.Close()
}
