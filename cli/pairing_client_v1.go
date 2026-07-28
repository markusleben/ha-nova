package main

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bytemare/ksf"
	"github.com/bytemare/opaque"
	"golang.org/x/crypto/hkdf"
)

// Client half of the secure device-pairing protocol. It mirrors the relay's
// nova/src/security modules and is proven byte-compatible with them:
//   - OPAQUE (RFC 9807) over the plain HTTP bootstrap port to authenticate the
//     one-time code and derive a session key without revealing the code;
//   - an application-layer AEAD (HKDF-SHA-512 -> directional AES-256-GCM) that
//     encrypts the client metadata and the provisioned-credential response, so
//     the credential never travels in clear over the HTTP bootstrap;
//   - SPKI-pinned TLS 1.3 for every later device call, trusting the relay only
//     by the exact pin delivered in the pairing response.

const (
	pairingProtocol       = "ha-nova-pair-v1"
	pairingClientID       = "ha-nova-device"
	pairingServerID       = "ha-nova-relay"
	maxPairingBodyBytes   = 64 << 10
	pairingHTTPTimeout    = 20 * time.Second
	pairingActivateRetry  = 3
	pairingFinishAttempts = 3
)

var pairB64 = base64.RawURLEncoding

type deviceMetadata struct {
	Name            string `json:"name"`
	Platform        string `json:"platform"`
	Client          string `json:"client"`
	ClientInstallID string `json:"client_install_id"`
}

type provisionedCredential struct {
	Credential string `json:"credential"`
	DeviceID   string `json:"device_id"`
	SpkiPin    string `json:"spki_pin"`
	SecurePort int    `json:"secure_port"`
}

func opaqueClientConfig() *opaque.Configuration {
	return &opaque.Configuration{
		Context: []byte{},
		KDF:     crypto.SHA512,
		MAC:     crypto.SHA512,
		Hash:    crypto.SHA512,
		KSF:     ksf.Argon2id,
		OPRF:    opaque.RistrettoSha512,
		AKE:     opaque.RistrettoSha512,
	}
}

// pairDeviceV1 runs the full OPAQUE bootstrap against the relay's plain HTTP
// port and returns the provisioned (not yet active) credential plus the secure
// endpoint the caller must pin for activation.
func pairDeviceV1(client *http.Client, bootstrapURL, code string, meta deviceMetadata) (*provisionedCredential, error) {
	if client == nil {
		client = &http.Client{Timeout: pairingHTTPTimeout}
	}
	// A trailing slash would make bootstrapURL+"/pair/v1/..." a double-slash path
	// the relay serves as 404; normalize once so every v1 request is well-formed.
	bootstrapURL = strings.TrimRight(bootstrapURL, "/")
	conf := opaqueClientConfig()
	oc, err := conf.Client()
	if err != nil {
		return nil, fmt.Errorf("opaque client: %w", err)
	}
	ke1, err := oc.GenerateKE1([]byte(code))
	if err != nil {
		return nil, fmt.Errorf("opaque KE1: %w", err)
	}

	var start struct {
		HandshakeID string `json:"handshake_id"`
		KE2         string `json:"ke2"`
	}
	if err := pairPostJSON(client, bootstrapURL+"/pair/v1/start", map[string]any{"ke1": pairB64.EncodeToString(ke1.Serialize())}, &start); err != nil {
		return nil, err
	}
	hsid, err := pairB64.DecodeString(start.HandshakeID)
	if err != nil {
		return nil, fmt.Errorf("bad handshake_id: %w", err)
	}
	ke2bytes, err := pairB64.DecodeString(start.KE2)
	if err != nil {
		return nil, fmt.Errorf("bad ke2: %w", err)
	}
	deser, err := conf.Deserializer()
	if err != nil {
		return nil, err
	}
	ke2, err := deser.KE2(ke2bytes)
	if err != nil {
		return nil, fmt.Errorf("opaque KE2: %w", err)
	}
	opts := &opaque.ClientOptions{KSFParameters: []uint64{3, 65536, 4}, KSFSalt: make([]byte, 16), KSFLength: 64}
	ke3, sessionKey, _, err := oc.GenerateKE3(ke2, []byte(pairingClientID), []byte(pairingServerID), opts)
	if err != nil {
		// A wrong code fails here; the caller shows a generic "code invalid".
		return nil, errPairingCodeRejected
	}

	c2s := derivePairKey(sessionKey, hsid, "c2s")
	s2c := derivePairKey(sessionKey, hsid, "s2c")
	metaJSON, _ := json.Marshal(meta)
	encMeta := pairB64.EncodeToString(pairSeal(c2s, hsid, "c2s", metaJSON))

	var finish struct {
		Response string `json:"response"`
	}
	if err := pairFinishPostJSON(client, bootstrapURL+"/pair/v1/finish", map[string]any{
		"handshake_id": start.HandshakeID,
		"ke3":          pairB64.EncodeToString(ke3.Serialize()),
		"metadata":     encMeta,
	}, &finish); err != nil {
		return nil, err
	}
	frame, err := pairB64.DecodeString(finish.Response)
	if err != nil {
		return nil, fmt.Errorf("bad response: %w", err)
	}
	plain, ok := pairOpen(s2c, hsid, "s2c", frame)
	if !ok {
		return nil, fmt.Errorf("could not decrypt pairing response")
	}
	var prov provisionedCredential
	if err := json.Unmarshal(plain, &prov); err != nil {
		return nil, fmt.Errorf("bad provisioned credential: %w", err)
	}
	if parseDeviceCredential(prov.Credential) == nil || prov.SpkiPin == "" || prov.SecurePort <= 0 {
		return nil, fmt.Errorf("relay returned an invalid provisioned credential")
	}
	return &prov, nil
}

// spkiPinnedClient trusts the relay's TLS listener ONLY when its leaf SPKI
// SHA-256 equals expectedPin — no CA chain, no "trust anyway".
func spkiPinnedClient(expectedPin string) *http.Client {
	return &http.Client{
		Timeout: pairingHTTPTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // pin check below is the trust anchor
				MinVersion:         tls.VersionTLS13,
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return fmt.Errorf("no server certificate")
					}
					cert, err := x509.ParseCertificate(rawCerts[0])
					if err != nil {
						return err
					}
					sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
					if pairB64.EncodeToString(sum[:]) != expectedPin {
						return errPinMismatch
					}
					return nil
				},
			},
		},
	}
}

func activateDeviceV1(secureBaseURL, spkiPin, credential string) error {
	client := spkiPinnedClient(spkiPin)
	var lastErr error
	for attempt := 0; attempt < pairingActivateRetry; attempt++ {
		status, err := pairAuthedPost(client, secureBaseURL+"/auth/device/activate", credential)
		if err == nil && status == http.StatusOK {
			return nil
		}
		if status == http.StatusUnauthorized {
			return errDeviceUnauthorized
		}
		// A non-200/non-401 response (e.g. 500 at the active-device cap, 404 on a
		// bad proxy path) has no transport error, so record the status instead of
		// leaving lastErr nil and reporting "%!w(<nil>)".
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("relay returned status %d", status)
		}
	}
	return fmt.Errorf("activation failed: %w", lastErr)
}

// revokeSelfDeviceV1 revokes the active credential. A 401 counts as success:
// the credential is already gone (a lost prior response).
func revokeSelfDeviceV1(secureBaseURL, spkiPin, credential string) error {
	client := spkiPinnedClient(spkiPin)
	status, err := pairAuthedPost(client, secureBaseURL+"/auth/device/revoke-self", credential)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusUnauthorized {
		return nil
	}
	return fmt.Errorf("revoke-self returned %d", status)
}

// ---- AEAD (matches nova/src/security/pairing-crypto.ts) ----

func derivePairKey(sessionKey, handshakeID []byte, dir string) []byte {
	r := hkdf.New(sha512.New, sessionKey, handshakeID, []byte(pairingProtocol+":"+dir))
	k := make([]byte, 32)
	_, _ = io.ReadFull(r, k)
	return k
}

func pairAAD(handshakeID []byte, dir string) []byte {
	return []byte(pairingProtocol + "|" + dir + "|" + pairB64.EncodeToString(handshakeID))
}

func pairSeal(key, handshakeID []byte, dir string, pt []byte) []byte {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	return append(nonce, gcm.Seal(nil, nonce, pt, pairAAD(handshakeID, dir))...)
}

func pairOpen(key, handshakeID []byte, dir string, blob []byte) ([]byte, bool) {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if len(blob) < gcm.NonceSize()+16 {
		return nil, false
	}
	pt, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], pairAAD(handshakeID, dir))
	return pt, err == nil
}

// ---- HTTP helpers ----

func pairPostJSON(client *http.Client, url string, body map[string]any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode pairing request: %w", err)
	}
	_, err = pairPostJSONEncoded(client, url, encoded, out)
	return err
}

// pairFinishPostJSON retries only outcomes where the relay may have committed
// the pending credential without the client receiving its durable response.
// The body is encoded once: every retry carries the exact same handshake id,
// KE3, metadata ciphertext, and wire bytes required by the relay's replay key.
func pairFinishPostJSON(
	client *http.Client,
	url string,
	body map[string]any,
	out any,
) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode pairing request: %w", err)
	}
	return retryPairingFinish(func() (bool, error) {
		return pairPostJSONEncoded(client, url, encoded, out)
	})
}

func retryPairingFinish(attempt func() (bool, error)) error {
	var lastErr error
	ambiguous := false
	for count := 0; count < pairingFinishAttempts; count++ {
		retryable, err := attempt()
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			if ambiguous {
				return fmt.Errorf(
					"%w; a later finish response was: %v",
					errPairingOutcomeUnknown,
					err,
				)
			}
			return err
		}
		ambiguous = true
	}
	if ambiguous {
		return fmt.Errorf("%w: %v", errPairingOutcomeUnknown, lastErr)
	}
	return lastErr
}

// pairPostJSONEncoded reports whether failure is ambiguous and therefore safe
// for an exact finish replay. A known 3xx/4xx or malformed success response is
// definitive and must never be retried.
func pairPostJSONEncoded(
	client *http.Client,
	url string,
	encoded []byte,
	out any,
) (bool, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return false, fmt.Errorf("build pairing request: %w", err)
	}
	// Never forward copied URL userinfo (http://user:pass@host) as a Basic auth
	// header, and keep it out of the error text.
	req.URL.User = nil
	req.Header.Set("Content-Type", "application/json")
	var wroteRequest atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) {
			wroteRequest.Store(true)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	resp, err := client.Do(req)
	if err != nil {
		if resp != nil {
			if resp.Body != nil {
				resp.Body.Close()
			}
			return false, fmt.Errorf("post %s: %w", req.URL.String(), err)
		}
		return wroteRequest.Load(), fmt.Errorf(
			"post %s: %w",
			req.URL.String(),
			err,
		)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxPairingBodyBytes))
	if resp.StatusCode != http.StatusOK {
		statusErr := pairingStatusError(
			resp.StatusCode,
			raw,
			resp.Header.Get("Retry-After"),
		)
		return resp.StatusCode >= http.StatusInternalServerError &&
			resp.StatusCode <= 599, statusErr
	}
	if readErr != nil {
		return true, fmt.Errorf("read pairing response: %w", readErr)
	}
	var env struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		return false, fmt.Errorf("unexpected relay response")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return false, err
	}
	return false, nil
}

func pairAuthedPost(client *http.Client, url, credential string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("authorization", "Bearer "+credential)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxPairingBodyBytes))
	return resp.StatusCode, nil
}
