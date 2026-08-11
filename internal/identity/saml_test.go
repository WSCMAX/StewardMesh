package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
)

type fakeSAMLAuthenticator struct {
	relayState        string
	expectedRequestID string
	principal         SAMLPrincipal
}

func (f *fakeSAMLAuthenticator) AuthenticationURL(relayState string) (string, string, error) {
	f.relayState = relayState
	return "https://identity.example.test/sso?RelayState=" + relayState, "id-authentication-request", nil
}

func (f *fakeSAMLAuthenticator) Authenticate(_ *http.Request, expectedRequestID string) (SAMLPrincipal, error) {
	f.expectedRequestID = expectedRequestID
	return f.principal, nil
}

func (*fakeSAMLAuthenticator) Metadata() ([]byte, error) {
	return []byte("<EntityDescriptor />"), nil
}

func TestSAMLFlowUsesBoundedOpaqueRelayStateAndRequestID(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	authenticator := &fakeSAMLAuthenticator{principal: SAMLPrincipal{
		Issuer: "https://identity.example.test", Subject: "persistent-subject",
		Email: "person@example.test", DisplayName: "Example Person",
	}}
	flow, err := NewSAMLFlow(authenticator, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, relayState, requestID, expiresAt, err := flow.Start()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(relayState)
	if err != nil || len(decoded) != 32 || len(relayState) > 80 {
		t.Fatalf("unexpected RelayState %q decoded=%d err=%v", relayState, len(decoded), err)
	}
	if requestID != "id-authentication-request" || authenticator.relayState != relayState ||
		!strings.Contains(authorizationURL, relayState) || !expiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unexpected SAML start url=%q request=%q expiry=%s", authorizationURL, requestID, expiresAt)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://stewardmesh.example.test/api/v1/auth/saml/acs", nil)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := flow.Complete(request, requestID)
	if err != nil || principal.Subject != authenticator.principal.Subject || authenticator.expectedRequestID != requestID {
		t.Fatalf("unexpected SAML completion %#v expected=%q err=%v", principal, authenticator.expectedRequestID, err)
	}
	metadata, err := flow.Metadata()
	if err != nil || string(metadata) != "<EntityDescriptor />" {
		t.Fatalf("unexpected metadata %q err=%v", metadata, err)
	}
}

func TestSAMLPrincipalUsesStableNameIDAndExactAttributeMapping(t *testing.T) {
	client := &SAMLClient{
		providerID:             "https://identity.example.test/saml",
		emailAttribute:         "mail",
		displayNameAttribute:   "displayName",
		administratorAttribute: "groups",
		administratorValues:    map[string]struct{}{"StewardMesh-Administrators": {}},
	}
	assertion := &saml.Assertion{
		Subject: &saml.Subject{NameID: &saml.NameID{Value: "persistent-subject"}},
		AttributeStatements: []saml.AttributeStatement{{Attributes: []saml.Attribute{
			{Name: "mail", Values: []saml.AttributeValue{{Value: " Person@Example.Test "}}},
			{FriendlyName: "displayName", Values: []saml.AttributeValue{{Value: " Example Person "}}},
			{Name: "groups", Values: []saml.AttributeValue{{Value: "stewardmesh-administrators"}, {Value: "StewardMesh-Administrators"}}},
		}}},
	}
	principal, err := client.principalFromAssertion(assertion)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Issuer != client.providerID || principal.Subject != "persistent-subject" ||
		principal.Email != "person@example.test" || principal.DisplayName != "Example Person" || !principal.Administrator {
		t.Fatalf("unexpected SAML principal %#v", principal)
	}
	client.administratorValues = map[string]struct{}{"STEWARDMESH-ADMINISTRATORS": {}}
	principal, err = client.principalFromAssertion(assertion)
	if err != nil || principal.Administrator {
		t.Fatalf("administrator mapping must be exact and case-sensitive: %#v err=%v", principal, err)
	}
}

func TestSAMLPrincipalAllowsEmailNameIDFallbackWithoutPersistingAssertion(t *testing.T) {
	client := &SAMLClient{
		providerID:           "https://identity.example.test/saml",
		emailAttribute:       "mail",
		displayNameAttribute: "displayName",
	}
	assertion := &saml.Assertion{Subject: &saml.Subject{NameID: &saml.NameID{
		Format: string(saml.EmailAddressNameIDFormat), Value: "person@example.test",
	}}}
	principal, err := client.principalFromAssertion(assertion)
	if err != nil || principal.Email != "person@example.test" || principal.DisplayName != principal.Email {
		t.Fatalf("unexpected email NameID fallback %#v err=%v", principal, err)
	}
}

func TestNewSAMLClientLoadsMetadataPublishesSPMetadataAndSignsRequest(t *testing.T) {
	certificateDER, certificatePEM, privateKeyPEM := testSAMLCertificate(t)
	directory := t.TempDir()
	certificateFile := directory + "/service-provider.crt"
	privateKeyFile := directory + "/service-provider.key"
	if err := os.WriteFile(certificateFile, certificatePEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privateKeyFile, privateKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	var metadataServer *httptest.Server
	metadataServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/metadata" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		_, _ = fmt.Fprintf(w, `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://identity.example.test/saml"><IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"><KeyDescriptor use="signing"><ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:X509Data><ds:X509Certificate>%s</ds:X509Certificate></ds:X509Data></ds:KeyInfo></KeyDescriptor><SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="%s/sso"/></IDPSSODescriptor></EntityDescriptor>`, base64.StdEncoding.EncodeToString(certificateDER), metadataServer.URL)
	}))
	defer metadataServer.Close()

	client, err := NewSAMLClient(context.Background(), SAMLConfig{
		IDPMetadataURL:  metadataServer.URL + "/metadata",
		EntityID:        "http://localhost:5173/api/v1/auth/saml/metadata",
		MetadataURL:     "http://localhost:5173/api/v1/auth/saml/metadata",
		ACSURL:          "http://localhost:5173/api/v1/auth/saml/acs",
		CertificateFile: certificateFile,
		PrivateKeyFile:  privateKeyFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	relayState := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	authorizationURL, requestID, err := client.AuthenticationURL(relayState)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if requestID == "" || parsed.Path != "/sso" || parsed.Query().Get("RelayState") != relayState ||
		parsed.Query().Get("SAMLRequest") == "" || parsed.Query().Get("SigAlg") != dsig.RSASHA256SignatureMethod ||
		parsed.Query().Get("Signature") == "" {
		t.Fatalf("unexpected signed SAML request id=%q url=%q", requestID, authorizationURL)
	}
	metadata, err := client.Metadata()
	if err != nil || !strings.Contains(string(metadata), `entityID="http://localhost:5173/api/v1/auth/saml/metadata"`) ||
		!strings.Contains(string(metadata), `AuthnRequestsSigned="true"`) {
		t.Fatalf("unexpected SP metadata %s err=%v", metadata, err)
	}
}

func testSAMLCertificate(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "stewardmesh-saml-test"},
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return certificateDER, certificatePEM, privateKeyPEM
}
