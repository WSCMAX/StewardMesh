package identity

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
)

const (
	defaultSAMLRequestTTL       = 10 * time.Minute
	maximumSAMLMetadataBytes    = 1 << 20
	defaultSAMLEmailAttribute   = "urn:oid:0.9.2342.19200300.100.1.3"
	defaultSAMLDisplayAttribute = "urn:oid:2.16.840.1.113730.3.1.241"
)

var ErrSAMLAuthentication = errors.New("SAML authentication failed")

// SAMLConfig describes one SAML 2.0 service provider. Attribute names are
// exact matches against Name or FriendlyName values from a verified assertion.
type SAMLConfig struct {
	IDPMetadataURL         string
	EntityID               string
	MetadataURL            string
	ACSURL                 string
	CertificateFile        string
	PrivateKeyFile         string
	EmailAttribute         string
	DisplayNameAttribute   string
	AdministratorAttribute string
	AdministratorValues    []string
}

// SAMLPrincipal contains only the normalized identity data Guard needs for
// just-in-time provisioning. The source assertion is never retained.
type SAMLPrincipal struct {
	Issuer        string
	Subject       string
	Email         string
	DisplayName   string
	Administrator bool
}

// SAMLAuthenticator is the protocol boundary used by SAMLFlow and replaced by
// deterministic fakes in unit and HTTP tests.
type SAMLAuthenticator interface {
	AuthenticationURL(relayState string) (authorizationURL, requestID string, err error)
	Authenticate(request *http.Request, expectedRequestID string) (SAMLPrincipal, error)
	Metadata() ([]byte, error)
}

type SAMLClient struct {
	serviceProvider        *saml.ServiceProvider
	providerID             string
	emailAttribute         string
	displayNameAttribute   string
	administratorAttribute string
	administratorValues    map[string]struct{}
}

func NewSAMLClient(ctx context.Context, configuration SAMLConfig) (*SAMLClient, error) {
	if ctx == nil {
		return nil, errors.New("initialize SAML: context is required")
	}
	configuration.IDPMetadataURL = strings.TrimSpace(configuration.IDPMetadataURL)
	configuration.EntityID = strings.TrimSpace(configuration.EntityID)
	configuration.MetadataURL = strings.TrimSpace(configuration.MetadataURL)
	configuration.ACSURL = strings.TrimSpace(configuration.ACSURL)
	configuration.CertificateFile = strings.TrimSpace(configuration.CertificateFile)
	configuration.PrivateKeyFile = strings.TrimSpace(configuration.PrivateKeyFile)
	configuration.EmailAttribute = strings.TrimSpace(configuration.EmailAttribute)
	configuration.DisplayNameAttribute = strings.TrimSpace(configuration.DisplayNameAttribute)
	configuration.AdministratorAttribute = strings.TrimSpace(configuration.AdministratorAttribute)
	if err := validateSAMLEndpoint(configuration.IDPMetadataURL); err != nil {
		return nil, fmt.Errorf("initialize SAML: IdP metadata URL is invalid: %w", err)
	}
	if err := validateSAMLEndpoint(configuration.MetadataURL); err != nil {
		return nil, fmt.Errorf("initialize SAML: metadata URL is invalid: %w", err)
	}
	if err := validateSAMLEndpoint(configuration.ACSURL); err != nil {
		return nil, fmt.Errorf("initialize SAML: ACS URL is invalid: %w", err)
	}
	if configuration.EntityID == "" || len(configuration.EntityID) > 2048 ||
		configuration.CertificateFile == "" || configuration.PrivateKeyFile == "" {
		return nil, errors.New("initialize SAML: entity id, certificate file, and private key file are required")
	}
	if configuration.EmailAttribute == "" {
		configuration.EmailAttribute = defaultSAMLEmailAttribute
	}
	if configuration.DisplayNameAttribute == "" {
		configuration.DisplayNameAttribute = defaultSAMLDisplayAttribute
	}
	for _, attribute := range []string{
		configuration.EmailAttribute,
		configuration.DisplayNameAttribute,
		configuration.AdministratorAttribute,
	} {
		if len(attribute) > 512 || strings.ContainsAny(attribute, "\r\n") {
			return nil, errors.New("initialize SAML: attribute names must be at most 512 characters and single-line")
		}
	}

	keyPair, err := tls.LoadX509KeyPair(configuration.CertificateFile, configuration.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("initialize SAML: load service-provider certificate and private key")
	}
	certificate, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, errors.New("initialize SAML: parse service-provider certificate")
	}
	now := time.Now().UTC()
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return nil, errors.New("initialize SAML: service-provider certificate is not currently valid")
	}
	signer, ok := keyPair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("initialize SAML: service-provider private key cannot sign requests")
	}
	signatureMethod, err := samlSignatureMethod(signer)
	if err != nil {
		return nil, err
	}
	httpClient, err := samlHTTPClient(configuration.IDPMetadataURL)
	if err != nil {
		return nil, err
	}
	idpMetadata, err := fetchSAMLMetadata(ctx, httpClient, configuration.IDPMetadataURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(idpMetadata.EntityID) == "" || len(idpMetadata.EntityID) > 2048 {
		return nil, errors.New("initialize SAML: IdP metadata has an invalid entity id")
	}
	if err := validateSAMLIDPMetadata(idpMetadata, now); err != nil {
		return nil, err
	}
	metadataURL, _ := url.Parse(configuration.MetadataURL)
	acsURL, _ := url.Parse(configuration.ACSURL)
	serviceProvider := &saml.ServiceProvider{
		EntityID:          configuration.EntityID,
		Key:               signer,
		Certificate:       certificate,
		HTTPClient:        httpClient,
		MetadataURL:       *metadataURL,
		AcsURL:            *acsURL,
		IDPMetadata:       idpMetadata,
		AuthnNameIDFormat: saml.PersistentNameIDFormat,
		SignatureMethod:   signatureMethod,
		AllowIDPInitiated: false,
	}
	ssoURL := serviceProvider.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if ssoURL == "" {
		return nil, errors.New("initialize SAML: IdP metadata does not advertise HTTP-Redirect SSO")
	}
	if err := validateSAMLEndpoint(ssoURL); err != nil {
		return nil, fmt.Errorf("initialize SAML: IdP SSO URL is invalid: %w", err)
	}
	administratorValues := make(map[string]struct{}, len(configuration.AdministratorValues))
	for _, value := range configuration.AdministratorValues {
		if value = strings.TrimSpace(value); value != "" {
			administratorValues[value] = struct{}{}
		}
	}
	return &SAMLClient{
		serviceProvider:        serviceProvider,
		providerID:             idpMetadata.EntityID,
		emailAttribute:         configuration.EmailAttribute,
		displayNameAttribute:   configuration.DisplayNameAttribute,
		administratorAttribute: configuration.AdministratorAttribute,
		administratorValues:    administratorValues,
	}, nil
}

func (c *SAMLClient) AuthenticationURL(relayState string) (string, string, error) {
	if c == nil || c.serviceProvider == nil || relayState == "" || len(relayState) > 80 {
		return "", "", ErrSAMLAuthentication
	}
	if !samlCertificateCurrentlyValid(c.serviceProvider.Certificate, time.Now().UTC()) {
		return "", "", ErrSAMLAuthentication
	}
	request, err := c.serviceProvider.MakeAuthenticationRequest(
		c.serviceProvider.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", "", ErrSAMLAuthentication
	}
	authorizationURL, err := request.Redirect(relayState, c.serviceProvider)
	if err != nil {
		return "", "", ErrSAMLAuthentication
	}
	return authorizationURL.String(), request.ID, nil
}

func (c *SAMLClient) Authenticate(request *http.Request, expectedRequestID string) (SAMLPrincipal, error) {
	if c == nil || c.serviceProvider == nil || request == nil || request.Context() == nil ||
		request.Method != http.MethodPost || strings.TrimSpace(expectedRequestID) == "" {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	if err := validateSAMLIDPMetadata(c.serviceProvider.IDPMetadata, time.Now().UTC()); err != nil {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	assertion, err := c.serviceProvider.ParseResponse(request, []string{expectedRequestID})
	if err != nil {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	principal, err := c.principalFromAssertion(assertion)
	if err != nil {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	return principal, nil
}

func (c *SAMLClient) Metadata() ([]byte, error) {
	if c == nil || c.serviceProvider == nil {
		return nil, ErrSAMLAuthentication
	}
	if !samlCertificateCurrentlyValid(c.serviceProvider.Certificate, time.Now().UTC()) {
		return nil, ErrSAMLAuthentication
	}
	encoded, err := xml.MarshalIndent(c.serviceProvider.Metadata(), "", "  ")
	if err != nil {
		return nil, errors.New("encode SAML service-provider metadata")
	}
	return append([]byte(xml.Header), encoded...), nil
}

func (c *SAMLClient) principalFromAssertion(assertion *saml.Assertion) (SAMLPrincipal, error) {
	if assertion == nil || assertion.Subject == nil || assertion.Subject.NameID == nil {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	subject := strings.TrimSpace(assertion.Subject.NameID.Value)
	email := strings.ToLower(firstSAMLAttribute(assertion, c.emailAttribute))
	if email == "" && assertion.Subject.NameID.Format == string(saml.EmailAddressNameIDFormat) {
		email = strings.ToLower(subject)
	}
	displayName := firstSAMLAttribute(assertion, c.displayNameAttribute)
	if displayName == "" {
		displayName = email
	}
	principal := SAMLPrincipal{
		Issuer:      strings.TrimSpace(c.providerID),
		Subject:     subject,
		Email:       email,
		DisplayName: displayName,
	}
	if principal.Issuer == "" || principal.Subject == "" || principal.Email == "" || principal.DisplayName == "" {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	for _, value := range samlAttributeValues(assertion, c.administratorAttribute) {
		if _, ok := c.administratorValues[value]; ok {
			principal.Administrator = true
			break
		}
	}
	return principal, nil
}

type SAMLFlow struct {
	authenticator SAMLAuthenticator
	now           func() time.Time
	requestTTL    time.Duration
}

func NewSAMLFlow(authenticator SAMLAuthenticator, now func() time.Time) (*SAMLFlow, error) {
	if authenticator == nil {
		return nil, errors.New("SAML authenticator is required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SAMLFlow{authenticator: authenticator, now: now, requestTTL: defaultSAMLRequestTTL}, nil
}

func (f *SAMLFlow) Start() (authorizationURL, relayState, requestID string, expiresAt time.Time, err error) {
	if f == nil {
		return "", "", "", time.Time{}, ErrSAMLAuthentication
	}
	relayState, err = randomSAMLValue()
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	authorizationURL, requestID, err = f.authenticator.AuthenticationURL(relayState)
	if err != nil {
		return "", "", "", time.Time{}, ErrSAMLAuthentication
	}
	return authorizationURL, relayState, requestID, f.now().Add(f.requestTTL), nil
}

func (f *SAMLFlow) Complete(request *http.Request, expectedRequestID string) (SAMLPrincipal, error) {
	if f == nil || request == nil || strings.TrimSpace(expectedRequestID) == "" {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	principal, err := f.authenticator.Authenticate(request, expectedRequestID)
	if err != nil {
		return SAMLPrincipal{}, ErrSAMLAuthentication
	}
	return principal, nil
}

func (f *SAMLFlow) Metadata() ([]byte, error) {
	if f == nil {
		return nil, ErrSAMLAuthentication
	}
	return f.authenticator.Metadata()
}

func randomSAMLValue() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", errors.New("generate SAML RelayState")
	}
	value := base64.RawURLEncoding.EncodeToString(buffer)
	clear(buffer)
	return value, nil
}

func samlAttributeValues(assertion *saml.Assertion, name string) []string {
	if assertion == nil || name == "" {
		return nil
	}
	var values []string
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			if attribute.Name != name && attribute.FriendlyName != name {
				continue
			}
			for _, value := range attribute.Values {
				if trimmed := strings.TrimSpace(value.Value); trimmed != "" {
					values = append(values, trimmed)
				}
			}
		}
	}
	return values
}

func firstSAMLAttribute(assertion *saml.Assertion, name string) string {
	values := samlAttributeValues(assertion, name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func samlSignatureMethod(signer crypto.Signer) (string, error) {
	switch signer.(type) {
	case *rsa.PrivateKey:
		return dsig.RSASHA256SignatureMethod, nil
	case *ecdsa.PrivateKey:
		return dsig.ECDSASHA256SignatureMethod, nil
	default:
		return "", fmt.Errorf("initialize SAML: unsupported private key type %T", signer)
	}
}

func samlHTTPClient(rawMetadataURL string) (*http.Client, error) {
	metadataURL, err := url.Parse(rawMetadataURL)
	if err != nil {
		return nil, errors.New("initialize SAML: parse IdP metadata URL")
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 2 || request.URL.Scheme != metadataURL.Scheme || request.URL.Host != metadataURL.Host {
				return errors.New("SAML metadata redirect is not allowed")
			}
			return nil
		},
	}, nil
}

func fetchSAMLMetadata(ctx context.Context, client *http.Client, rawMetadataURL string) (*saml.EntityDescriptor, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawMetadataURL, nil)
	if err != nil {
		return nil, errors.New("initialize SAML: create IdP metadata request")
	}
	request.Header.Set("Accept", "application/samlmetadata+xml, application/xml;q=0.9")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("initialize SAML: fetch IdP metadata")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("initialize SAML: IdP metadata returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumSAMLMetadataBytes+1))
	if err != nil || len(data) == 0 || len(data) > maximumSAMLMetadataBytes {
		return nil, errors.New("initialize SAML: IdP metadata is empty, unreadable, or too large")
	}
	metadata, err := samlsp.ParseMetadata(data)
	if err != nil {
		return nil, errors.New("initialize SAML: parse IdP metadata")
	}
	return metadata, nil
}

func validateSAMLIDPMetadata(metadata *saml.EntityDescriptor, now time.Time) error {
	if metadata == nil || len(metadata.IDPSSODescriptors) == 0 {
		return errors.New("initialize SAML: IdP metadata has no SSO descriptor")
	}
	if !metadata.ValidUntil.IsZero() && !metadata.ValidUntil.After(now) {
		return errors.New("initialize SAML: IdP metadata has expired")
	}
	for _, descriptor := range metadata.IDPSSODescriptors {
		if descriptor.ValidUntil != nil && !descriptor.ValidUntil.After(now) {
			return errors.New("initialize SAML: IdP SSO metadata has expired")
		}
		for _, key := range descriptor.KeyDescriptors {
			if key.Use != "" && key.Use != "signing" {
				continue
			}
			for _, encoded := range key.KeyInfo.X509Data.X509Certificates {
				der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded.Data), ""))
				if err != nil {
					continue
				}
				certificate, err := x509.ParseCertificate(der)
				if err == nil && samlCertificateCurrentlyValid(certificate, now) {
					return nil
				}
			}
		}
	}
	return errors.New("initialize SAML: IdP metadata has no currently valid signing certificate")
}

func samlCertificateCurrentlyValid(certificate *x509.Certificate, now time.Time) bool {
	return certificate != nil && !now.Before(certificate.NotBefore) && now.Before(certificate.NotAfter)
}

func validateSAMLEndpoint(raw string) error {
	return validateOIDCEndpoint(strings.TrimSpace(raw))
}
