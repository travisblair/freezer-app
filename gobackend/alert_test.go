package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpEvent is one client-to-server command observed by the test server.
type smtpEvent string

const (
	evSTARTTLS      smtpEvent = "starttls"
	evAuthBeforeTLS smtpEvent = "auth-before-tls"
	evAuthAfterTLS  smtpEvent = "auth-after-tls"
	evMailAfterTLS  smtpEvent = "mail-after-tls"
)

// startTestSMTPServer runs a minimal SMTP server that advertises STARTTLS
// (the Gmail-587 shape) and records the command sequence on `events`.
// TLS is enabled if advertiseSTARTTLS is true; the server does not require
// certificate trust — the client's verify result is irrelevant to the
// contract under test (STARTTLS must precede AUTH).
func startTestSMTPServer(t *testing.T, advertiseSTARTTLS bool) (addr string, events chan smtpEvent, closeFn func()) {
	t.Helper()

	cert, err := selfSignedCert(t)
	if err != nil {
		t.Fatalf("self-signed cert: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	events = make(chan smtpEvent, 16)

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		write := func(format string, args ...any) bool {
			_, err := fmt.Fprintf(w, format, args...)
			if err != nil {
				return false
			}
			return w.Flush() == nil
		}
		if !write("220 test ESMTP\r\n") {
			return
		}
		tlsActive := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return // client went away (normal after a verify failure)
			}
			line = strings.TrimSpace(line)
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				if advertiseSTARTTLS {
					if !write("250-test\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n") {
						return
					}
				} else {
					if !write("250-test\r\n250 AUTH PLAIN\r\n") {
						return
					}
				}
			case upper == "STARTTLS":
				events <- evSTARTTLS
				if !write("220 Ready to start TLS\r\n") {
					return
				}
				tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
				// The client may abort the handshake over our untrusted
				// cert; the STARTTLS command is what this test pins, so
				// continue serving either way.
				if err := tconn.Handshake(); err != nil {
					// Still record that TLS was negotiated up to the
					// client's abort — future commands, if any, are TLS.
					r = bufio.NewReader(tconn)
					w = bufio.NewWriter(tconn)
					tlsActive = true
				} else {
					r = bufio.NewReader(tconn)
					w = bufio.NewWriter(tconn)
					tlsActive = true
				}
			case strings.HasPrefix(upper, "AUTH"):
				if !tlsActive {
					events <- evAuthBeforeTLS
					if !write("530 Must issue STARTTLS first\r\n") {
						return
					}
				} else {
					events <- evAuthAfterTLS
					if !write("235 2.7.0 Accepted\r\n") {
						return
					}
				}
			case strings.HasPrefix(upper, "MAIL"):
				if tlsActive {
					events <- evMailAfterTLS
				}
				if !write("250 OK\r\n") {
					return
				}
			case strings.HasPrefix(upper, "RCPT"):
				if !write("250 OK\r\n") {
					return
				}
			case upper == "DATA":
				if !write("354 End data with <CR><LF>.<CR><LF>\r\n") {
					return
				}
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" || l == ".\n" {
						break
					}
				}
				if !write("250 OK: queued\r\n") {
					return
				}
			case upper == "QUIT":
				if !write("221 Bye\r\n") {
					return
				}
				return
			default:
				if !write("250 OK\r\n") {
					return
				}
			}
		}
	}()

	return ln.Addr().String(), events, func() {
		ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// selfSignedCert generates a throwaway cert for the TLS handshake. Its
// trust status is irrelevant: the test pins the SMTP command sequence.
func selfSignedCert(t *testing.T) (tls.Certificate, error) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return tls.X509KeyPair(certPEM, keyPEM)
}

// resetAlertConfigForTest re-reads ALERT_* env for one test. Caller must
// hold the package's config Once reset — do not use in parallel tests.
func resetAlertConfigForTest(t *testing.T) {
	t.Helper()
	alertCfgOnce = sync.Once{}
}

// drainEvents reads events until the deadline or the channel is empty.
func drainEvents(ch chan smtpEvent, wait time.Duration) []smtpEvent {
	var out []smtpEvent
	deadline := time.Now().Add(wait)
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(time.Until(deadline)):
			return out
		}
	}
}

// TestTrySendAlertUsesStartTLS pins the Aug-31 regression: the manual SMTP
// rewrite dropped STARTTLS, so smtp.PlainAuth refused credentials
// ("unencrypted connection") and every alert failed silently. Against a
// server that advertises STARTTLS, the client must issue STARTTLS before
// AUTH — with the regression it never sends either.
func TestTrySendAlertUsesStartTLS(t *testing.T) {
	addr, events, closeFn := startTestSMTPServer(t, true)
	defer closeFn()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}

	t.Setenv("ALERT_SMTP_HOST", host)
	t.Setenv("ALERT_SMTP_PORT", port)
	t.Setenv("ALERT_EMAIL", "sender@test.local")
	t.Setenv("ALERT_APP_PASSWORD", "test-password")
	t.Setenv("ALERT_TO", "receiver@test.local")
	resetAlertConfigForTest(t)

	_ = trySendAlert("test subject", "test body") // error is the untrusted-cert result; irrelevant here

	got := drainEvents(events, 2*time.Second)
	hasSTARTTLS := false
	for _, e := range got {
		if e == evSTARTTLS {
			hasSTARTTLS = true
		}
		if e == evAuthBeforeTLS {
			t.Fatalf("client sent AUTH on plaintext: %v", got)
		}
	}
	if !hasSTARTTLS {
		t.Fatalf("client never issued STARTTLS (regression). events: %v", got)
	}
}
