package common

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewSMTPClientUsesOpportunisticSTARTTLS(t *testing.T) {
	restore := preserveSMTPGlobals()
	defer restore()

	cert := testTLSCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	SMTPServer = "localhost"
	SMTPPort = mustPort(t, listener.Addr().String())
	SMTPSSLEnabled = false
	SMTPStartTLSEnabled = false
	SMTPInsecureSkipVerify = true

	done := make(chan error, 1)
	upgraded := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- serveStartTLSSMTP(conn, cert, upgraded)
	}()

	client, err := newSMTPClient(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.TLSConnectionState(); !ok {
		t.Fatal("expected opportunistic STARTTLS to upgrade the SMTP connection")
	}
	_ = client.Close()

	select {
	case <-upgraded:
	case <-time.After(time.Second):
		t.Fatal("SMTP server did not observe STARTTLS")
	}
	if err := waitSMTPServer(done); err != nil {
		t.Fatal(err)
	}
}

func TestNewSMTPClientRequiresSTARTTLSWhenConfigured(t *testing.T) {
	restore := preserveSMTPGlobals()
	defer restore()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	SMTPServer = "localhost"
	SMTPPort = mustPort(t, listener.Addr().String())
	SMTPSSLEnabled = false
	SMTPStartTLSEnabled = true

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- servePlainSMTP(conn, false, false)
	}()

	client, err := newSMTPClient(listener.Addr().String())
	if err == nil {
		_ = client.Close()
		t.Fatal("expected STARTTLS-required SMTP client to reject a server without STARTTLS")
	}
	if !errors.Is(err, ErrSMTPStartTLSUnsupported) {
		t.Fatalf("expected STARTTLS unsupported sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected STARTTLS error, got %v", err)
	}
	if err := waitSMTPServer(done); err != nil {
		t.Fatal(err)
	}
}

func TestNewSMTPClientTreatsPort465AsImplicitTLS(t *testing.T) {
	restore := preserveSMTPGlobals()
	defer restore()

	cert := testTLSCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	SMTPServer = "localhost"
	SMTPPort = 465
	SMTPSSLEnabled = false
	SMTPStartTLSEnabled = true
	SMTPInsecureSkipVerify = true

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- servePlainSMTP(conn, false, false)
	}()

	client, err := newSMTPClient(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := waitSMTPServer(done); err != nil {
		t.Fatal(err)
	}
}

func TestSendEmailIgnoresQuitErrorAfterDataAccepted(t *testing.T) {
	restore := preserveSMTPGlobals()
	defer restore()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	SMTPServer = "localhost"
	SMTPPort = mustPort(t, listener.Addr().String())
	SMTPSSLEnabled = false
	SMTPStartTLSEnabled = false
	SMTPInsecureSkipVerify = false
	SMTPAccount = ""
	SMTPToken = ""
	SMTPFrom = "sender@example.com"

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- servePlainSMTP(conn, false, true)
	}()

	if err := SendEmail("test", "receiver@example.com", "body"); err != nil {
		t.Fatalf("expected accepted email to ignore QUIT error, got %v", err)
	}
	if err := waitSMTPServer(done); err != nil {
		t.Fatal(err)
	}
}

func TestSendEmailSkipsAuthWhenServerDoesNotAdvertiseAuth(t *testing.T) {
	restore := preserveSMTPGlobals()
	defer restore()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	SMTPServer = "localhost"
	SMTPPort = mustPort(t, listener.Addr().String())
	SMTPSSLEnabled = false
	SMTPStartTLSEnabled = false
	SMTPInsecureSkipVerify = false
	SMTPForceAuthLogin = false
	SMTPAccount = "smtp-user@example.com"
	SMTPToken = "secret"
	SMTPFrom = "sender@example.com"

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- servePlainSMTP(conn, false, false)
	}()

	if err := SendEmail("test", "receiver@example.com", "body"); err != nil {
		t.Fatalf("expected email to be sent without AUTH when server does not advertise AUTH, got %v", err)
	}
	if err := waitSMTPServer(done); err != nil {
		t.Fatal(err)
	}
}

func TestNewSMTPClientUsesOperationDeadline(t *testing.T) {
	restore := preserveSMTPGlobals()
	defer restore()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	SMTPServer = "localhost"
	SMTPPort = mustPort(t, listener.Addr().String())
	SMTPSSLEnabled = false
	SMTPStartTLSEnabled = false

	oldTimeout := smtpOperationTimeout
	smtpOperationTimeout = 50 * time.Millisecond
	defer func() { smtpOperationTimeout = oldTimeout }()

	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer conn.Close()
		time.Sleep(200 * time.Millisecond)
		done <- nil
	}()

	started := time.Now()
	client, err := newSMTPClient(listener.Addr().String())
	if client != nil {
		_ = client.Close()
	}
	if err == nil {
		t.Fatal("expected SMTP greeting timeout")
	}
	if time.Since(started) >= time.Second {
		t.Fatalf("SMTP timeout took too long: %s", time.Since(started))
	}
	if err := waitSMTPServer(done); err != nil {
		t.Fatal(err)
	}
}

func preserveSMTPGlobals() func() {
	old := struct {
		server             string
		port               int
		sslEnabled         bool
		startTLSEnabled    bool
		insecureSkipVerify bool
		forceAuthLogin     bool
		account            string
		from               string
		token              string
	}{
		server:             SMTPServer,
		port:               SMTPPort,
		sslEnabled:         SMTPSSLEnabled,
		startTLSEnabled:    SMTPStartTLSEnabled,
		insecureSkipVerify: SMTPInsecureSkipVerify,
		forceAuthLogin:     SMTPForceAuthLogin,
		account:            SMTPAccount,
		from:               SMTPFrom,
		token:              SMTPToken,
	}
	return func() {
		SMTPServer = old.server
		SMTPPort = old.port
		SMTPSSLEnabled = old.sslEnabled
		SMTPStartTLSEnabled = old.startTLSEnabled
		SMTPInsecureSkipVerify = old.insecureSkipVerify
		SMTPForceAuthLogin = old.forceAuthLogin
		SMTPAccount = old.account
		SMTPFrom = old.from
		SMTPToken = old.token
	}
}

func serveStartTLSSMTP(conn net.Conn, cert tls.Certificate, upgraded chan<- struct{}) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	if err := smtpWrite(writer, "220 localhost ESMTP\r\n"); err != nil {
		return err
	}

	tlsActive := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			if tlsActive {
				if err := smtpWrite(writer, "250-localhost\r\n250 AUTH PLAIN LOGIN\r\n"); err != nil {
					return err
				}
			} else if err := smtpWrite(writer, "250-localhost\r\n250-STARTTLS\r\n250 AUTH PLAIN LOGIN\r\n"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "STARTTLS"):
			if err := smtpWrite(writer, "220 Ready to start TLS\r\n"); err != nil {
				return err
			}
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			if err := tlsConn.Handshake(); err != nil {
				return err
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
			writer = bufio.NewWriter(conn)
			tlsActive = true
			select {
			case upgraded <- struct{}{}:
			default:
			}
		case strings.HasPrefix(command, "QUIT"):
			return smtpWrite(writer, "221 Bye\r\n")
		default:
			if err := smtpWrite(writer, "250 OK\r\n"); err != nil {
				return err
			}
		}
	}
}

func servePlainSMTP(conn net.Conn, advertiseSTARTTLS bool, closeAfterData bool) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	if err := smtpWrite(writer, "220 localhost ESMTP\r\n"); err != nil {
		return err
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			if advertiseSTARTTLS {
				if err := smtpWrite(writer, "250-localhost\r\n250 STARTTLS\r\n"); err != nil {
					return err
				}
			} else if err := smtpWrite(writer, "250 localhost\r\n"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "MAIL"), strings.HasPrefix(command, "RCPT"):
			if err := smtpWrite(writer, "250 OK\r\n"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "DATA"):
			if err := smtpWrite(writer, "354 End data with <CR><LF>.<CR><LF>\r\n"); err != nil {
				return err
			}
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
			}
			if err := smtpWrite(writer, "250 OK queued\r\n"); err != nil {
				return err
			}
			if closeAfterData {
				return nil
			}
		case strings.HasPrefix(command, "QUIT"):
			return smtpWrite(writer, "221 Bye\r\n")
		default:
			if err := smtpWrite(writer, "250 OK\r\n"); err != nil {
				return err
			}
		}
	}
}

func smtpWrite(writer *bufio.Writer, value string) error {
	if _, err := writer.WriteString(value); err != nil {
		return err
	}
	return writer.Flush()
}

func waitSMTPServer(done <-chan error) error {
	select {
	case err := <-done:
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	case <-time.After(time.Second):
		return errors.New("SMTP test server did not finish")
	}
}

func mustPort(t *testing.T, addr string) int {
	t.Helper()

	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func testTLSCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour),
		DNSNames:  []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
		},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
