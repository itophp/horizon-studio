package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

func samlConfigFixture(t *testing.T, bits int, start, end time.Time) SAMLProviderConfig {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: start, NotAfter: end, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return SAMLProviderConfig{ID: "saml", Name: "SAML", EntityID: "https://console.test/saml",
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		PrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		MetadataXML: fmt.Sprintf(`<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test/metadata">
<IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
<KeyDescriptor use="signing"><KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>%s</X509Certificate></X509Data></KeyInfo></KeyDescriptor>
<SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.test/sso"/>
</IDPSSODescriptor></EntityDescriptor>`, base64.StdEncoding.EncodeToString(der)),
	}
}

func TestSAMLConfigurationTrustValidation(t *testing.T) {
	now := time.Now()
	valid := samlConfigFixture(t, 2048, now.Add(-time.Hour), now.Add(time.Hour))
	weak := samlConfigFixture(t, 1024, now.Add(-time.Hour), now.Add(time.Hour))
	expired := samlConfigFixture(t, 2048, now.Add(-2*time.Hour), now.Add(-time.Hour))
	future := samlConfigFixture(t, 2048, now.Add(time.Hour), now.Add(2*time.Hour))
	const origin = "https://console.test"
	if err := ValidateSAMLProviders([]SAMLProviderConfig{valid}, origin, true); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*SAMLProviderConfig){
		"provider path":    func(p *SAMLProviderConfig) { p.ID = "../saml" },
		"invalid metadata": func(p *SAMLProviderConfig) { p.MetadataXML = "not XML" },
		"missing signing key": func(p *SAMLProviderConfig) {
			p.MetadataXML = strings.ReplaceAll(p.MetadataXML, `use="signing"`, `use="encryption"`)
		},
		"malformed signing key": func(p *SAMLProviderConfig) {
			p.MetadataXML = strings.ReplaceAll(p.MetadataXML, "<X509Certificate>", "<X509Certificate>!")
		},
		"weak signing key":        func(p *SAMLProviderConfig) { p.MetadataXML = weak.MetadataXML },
		"expired signing key":     func(p *SAMLProviderConfig) { p.MetadataXML = expired.MetadataXML },
		"future signing key only": func(p *SAMLProviderConfig) { p.MetadataXML = future.MetadataXML },
		"expired metadata": func(p *SAMLProviderConfig) {
			p.MetadataXML = strings.Replace(p.MetadataXML, "entityID=", `validUntil="2000-01-01T00:00:00Z" entityID=`, 1)
		},
		"expired descriptor": func(p *SAMLProviderConfig) {
			p.MetadataXML = strings.Replace(p.MetadataXML, "<IDPSSODescriptor ", `<IDPSSODescriptor validUntil="2000-01-01T00:00:00Z" `, 1)
		},
		"insecure SSO": func(p *SAMLProviderConfig) {
			p.MetadataXML = strings.ReplaceAll(p.MetadataXML, "https://idp.test/sso", "http://idp.test/sso")
		},
		"SSO userinfo": func(p *SAMLProviderConfig) {
			p.MetadataXML = strings.ReplaceAll(p.MetadataXML, "https://idp.test/sso", "https://secret@idp.test/sso")
		},
		"SP mismatched key": func(p *SAMLProviderConfig) { p.PrivateKeyPEM = weak.PrivateKeyPEM },
		"SP weak key": func(p *SAMLProviderConfig) {
			p.PrivateKeyPEM, p.CertificatePEM = weak.PrivateKeyPEM, weak.CertificatePEM
		},
		"SP expired": func(p *SAMLProviderConfig) {
			p.PrivateKeyPEM, p.CertificatePEM = expired.PrivateKeyPEM, expired.CertificatePEM
		},
		"SP future": func(p *SAMLProviderConfig) {
			p.PrivateKeyPEM, p.CertificatePEM = future.PrivateKeyPEM, future.CertificatePEM
		},
		"entity userinfo": func(p *SAMLProviderConfig) { p.EntityID = "https://secret@console.test/entity" },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if err := ValidateSAMLProviders([]SAMLProviderConfig{config}, origin, true); err == nil {
				t.Fatal("unsafe SAML trust configuration accepted")
			}
		})
	}
	for _, origin := range []string{"http://127.0.0.1:3000", "https://console.test?secret=value", "https://console.test/path", "https://secret@console.test"} {
		if err := ValidateSAMLProviders([]SAMLProviderConfig{valid}, origin, false); err == nil {
			t.Fatal("SAML browser POST requires an HTTPS public origin even in development")
		}
	}
	if err := ValidateSAMLProviders([]SAMLProviderConfig{valid, valid}, origin, true); err == nil {
		t.Fatal("duplicate SAML IDs accepted")
	}
	// Unspecified key usage is valid for signing; metadata may wrap base64 lines.
	wrapped := valid
	wrapped.MetadataXML = strings.ReplaceAll(strings.ReplaceAll(valid.MetadataXML, `use="signing"`, ""), "<X509Certificate>", "<X509Certificate>\n  ")
	if err := ValidateSAMLProviders([]SAMLProviderConfig{wrapped}, origin, true); err != nil {
		t.Fatal(err)
	}
}

func TestFederationIDsAreUniqueInEitherConfigurationOrder(t *testing.T) {
	config := samlConfigFixture(t, 2048, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	s := &Service{SecureCookies: true, oidcProviders: map[string]*oidcProvider{config.ID: {}}}
	if err := s.ConfigureSAML([]SAMLProviderConfig{config}, "https://console.test"); err == nil {
		t.Fatal("SAML accepted an OIDC provider ID")
	}
	s.oidcProviders = nil
	if err := s.ConfigureSAML([]SAMLProviderConfig{config}, "https://console.test"); err != nil {
		t.Fatal(err)
	}
	err := s.ConfigureOIDC(context.Background(), []OIDCProviderConfig{{ID: config.ID, Name: "OIDC", Issuer: "https://idp.test", ClientID: "test", RedirectURL: "https://console.test/login/oidc"}}, "https://console.test")
	if err == nil || !strings.Contains(err.Error(), "unique across protocols") {
		t.Fatal("OIDC must reject a SAML provider ID before remote discovery")
	}
}
