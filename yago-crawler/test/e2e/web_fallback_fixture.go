//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"html"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	webFallbackAuthorityPath = "/opt/yago/data/web-fallback-fixture-ca.pem"
	webFallbackProviderHost  = "html.duckduckgo.com"
	webFallbackLiteHost      = "lite.duckduckgo.com"
)

type webFallbackFixture struct {
	authority []byte
	container testcontainers.Container
	rows      []webFallbackFixtureRow
}

type webFallbackFixtureRow struct {
	host     string
	path     string
	title    string
	snippet  string
	evidence string
}

type webFallbackCertificate struct {
	authority   []byte
	certificate []byte
	privateKey  []byte
}

func (fixture webFallbackFixture) nodeMounts(t *testing.T) testcontainers.ContainerMounts {
	t.Helper()
	authorityPath := filepath.Join(t.TempDir(), "web-fallback-fixture-ca.pem")
	if err := os.WriteFile(authorityPath, fixture.authority, 0o644); err != nil {
		t.Fatalf("write web fallback authority: %v", err)
	}

	return testcontainers.ContainerMounts{{
		Source:   testcontainers.GenericBindMountSource{HostPath: authorityPath},
		Target:   testcontainers.ContainerMountTarget(webFallbackAuthorityPath),
		ReadOnly: true,
	}}
}

func startWebFallbackFixture(
	t *testing.T,
	ctx context.Context,
	networkName string,
) webFallbackFixture {
	t.Helper()
	certificate := newWebFallbackCertificate(t)
	rows := webFallbackRows()
	files := []testcontainers.ContainerFile{
		{
			Reader:            strings.NewReader(webFallbackNginxConfiguration()),
			ContainerFilePath: "/etc/nginx/nginx.conf",
			FileMode:          0o644,
		},
		{
			Reader:            bytes.NewReader(certificate.certificate),
			ContainerFilePath: "/etc/nginx/server.crt",
			FileMode:          0o644,
		},
		{
			Reader:            bytes.NewReader(certificate.privateKey),
			ContainerFilePath: "/etc/nginx/server.key",
			FileMode:          0o600,
		},
		{
			Reader:            strings.NewReader(webFallbackProviderPage(rows)),
			ContainerFilePath: "/usr/share/nginx/html/ddgs.html",
			FileMode:          0o644,
		},
		{
			Reader:            strings.NewReader("ready"),
			ContainerFilePath: "/usr/share/nginx/html/ready.html",
			FileMode:          0o644,
		},
		{
			Reader:            strings.NewReader("User-agent: *\nDisallow:\n"),
			ContainerFilePath: "/usr/share/nginx/html/robots.txt",
			FileMode:          0o644,
		},
	}
	for _, row := range rows {
		files = append(files, testcontainers.ContainerFile{
			Reader:            strings.NewReader(webFallbackOriginPage(row)),
			ContainerFilePath: "/usr/share/nginx/html" + row.path,
			FileMode:          0o644,
		})
	}

	aliases := []string{webFallbackProviderHost, webFallbackLiteHost}
	for _, row := range rows {
		aliases = append(aliases, row.host)
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          originImage,
			ExposedPorts:   []string{"80/tcp", "443/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: aliases},
			Files:          files,
			WaitingFor: wait.ForHTTP("/ready.html").
				WithPort("80/tcp").
				WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start web fallback fixture: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "web-fallback-fixture", container)

	return webFallbackFixture{
		authority: certificate.authority,
		container: container,
		rows:      rows,
	}
}

func webFallbackRows() []webFallbackFixtureRow {
	return []webFallbackFixtureRow{
		{
			host:    "www.postgresql.org",
			path:    "/postgresql.html",
			title:   "PostgreSQL official documentation transactions stored procedures",
			snippet: "PostgreSQL official documentation for transactions and stored procedures.",
			evidence: "PostgreSQL documentation explains transactions, isolation, " +
				"stored procedures, queries, tables, and reliable database operation.",
		},
		{
			host:    "giraffe.example",
			path:    "/adult-giraffe.html",
			title:   "Adult giraffe male female average weight kg reliable source",
			snippet: "A reliable source for adult giraffe male and female average weight in kg.",
			evidence: "Adult giraffe weight differs between male and female animals. " +
				"This reliable reference reports average mass in kilograms.",
		},
		{
			host:    "ru-giraffe.example",
			path:    "/ves-vzroslogo-zhirafa.html",
			title:   "Вес взрослого жирафа: сколько весит самец и самка в кг",
			snippet: "Источник о том, сколько весит взрослый жираф, самец и самка.",
			evidence: "Вес взрослого жирафа указан отдельно для самца и самки. " +
				"Материал приводит среднюю массу в килограммах и источники данных.",
		},
		{
			host:    "materialize.example",
			path:    "/materialize.html",
			title:   "Materialization sentinel quasar web result",
			snippet: "Materialization sentinel quasar proves web discovery becomes a local document.",
			evidence: "Materialization sentinel quasar is a deterministic crawl " +
				"fixture whose extracted body must enter the local document store.",
		},
		{
			host:    "mouse-one.example",
			path:    "/gaming-mouse-one.html",
			title:   "Best mouse for gaming latency guide",
			snippet: "Best mouse for gaming comparison with measured latency and sensor evidence.",
			evidence: "This best mouse for gaming guide compares click latency, " +
				"sensor tracking, shape, mass, switches, and polling behavior.",
		},
		{
			host:    "mouse-two.example",
			path:    "/gaming-mouse-two.html",
			title:   "Best gaming mouse sensor measurements",
			snippet: "Independent measurements identify the best gaming mouse sensor behavior.",
			evidence: "Independent gaming mouse measurements compare sensor error, " +
				"motion delay, click response, firmware, and wireless stability.",
		},
		{
			host:    "mouse-three.example",
			path:    "/gaming-mouse-three.html",
			title:   "Gaming mouse buying guide for competitive play",
			snippet: "A gaming mouse buying guide for competitive players choosing the best fit.",
			evidence: "Competitive gaming mouse selection depends on grip, shape, " +
				"weight, sensor consistency, click feel, and measured response.",
		},
	}
}

func webFallbackProviderPage(rows []webFallbackFixtureRow) string {
	var page strings.Builder
	page.WriteString("<!doctype html><html><body>")
	for _, row := range rows {
		target := "http://" + row.host + row.path
		page.WriteString(`<div class="result results_links web-result"><div class="links_main">`)
		page.WriteString(`<a class="result__a" href="//duckduckgo.com/l/?uddg=`)
		page.WriteString(url.QueryEscape(target))
		page.WriteString(`&amp;rut=fixture">`)
		page.WriteString(html.EscapeString(row.title))
		page.WriteString(`</a><a class="result__snippet">`)
		page.WriteString(html.EscapeString(row.snippet))
		page.WriteString(`</a></div></div>`)
	}
	page.WriteString("</body></html>")

	return page.String()
}

func webFallbackOriginPage(row webFallbackFixtureRow) string {
	return fmt.Sprintf(
		"<!doctype html><html><head><title>%s</title></head><body><h1>%s</h1><p>%s %s</p></body></html>",
		html.EscapeString(row.title),
		html.EscapeString(row.title),
		html.EscapeString(row.evidence),
		restartIndexableText(240),
	)
}

func webFallbackNginxConfiguration() string {
	return `events {}
http {
  access_log /dev/stdout combined;
  error_log /dev/stderr notice;
  server {
    listen 443 ssl;
    server_name html.duckduckgo.com lite.duckduckgo.com;
    ssl_certificate /etc/nginx/server.crt;
    ssl_certificate_key /etc/nginx/server.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    root /usr/share/nginx/html;
    default_type text/html;
    location = /html/ {
      if ($arg_q ~* "incomplete.*miss") { return 503; }
      try_files /ddgs.html =404;
    }
    location = /lite/ {
      if ($arg_q ~* "incomplete.*miss") { return 503; }
      return 200 "<!doctype html><html><body></body></html>";
    }
  }
  server {
    listen 80 default_server;
    server_name _;
    root /usr/share/nginx/html;
    default_type text/html;
    location / {
      try_files $uri =404;
    }
  }
}`
}

func newWebFallbackCertificate(t *testing.T) webFallbackCertificate {
	t.Helper()
	authorityKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate web fallback authority key: %v", err)
	}
	now := time.Now()
	authorityTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "YaGo web fallback E2E authority"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	authorityDER, err := x509.CreateCertificate(
		rand.Reader,
		&authorityTemplate,
		&authorityTemplate,
		&authorityKey.PublicKey,
		authorityKey,
	)
	if err != nil {
		t.Fatalf("create web fallback authority: %v", err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate web fallback server key: %v", err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: webFallbackProviderHost},
		DNSNames:     []string{webFallbackProviderHost, webFallbackLiteHost},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader,
		&serverTemplate,
		&authorityTemplate,
		&serverKey.PublicKey,
		authorityKey,
	)
	if err != nil {
		t.Fatalf("create web fallback server certificate: %v", err)
	}

	return webFallbackCertificate{
		authority: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: authorityDER,
		}),
		certificate: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: serverDER,
		}),
		privateKey: pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(serverKey),
		}),
	}
}
