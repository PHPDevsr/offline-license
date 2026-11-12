package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadIssueRequest(t *testing.T) {
	const valid = `{"license_id":"license-1","installation_id":"install_2"}`
	got, err := readIssueRequest([]byte(valid))
	if err != nil || got != (issueRequest{LicenseID: "license-1", InstallationID: "install_2"}) {
		t.Fatalf("valid request: %+v, %v", got, err)
	}

	for _, body := range []string{
		`null`,
		`{"installation_id":"install_2"}`,
		`{"license_id":"license-1","installation_id":null}`,
		`{"license_id":"license-1","installation_id":"../install_2"}`,
		`{"license_id":"license-1","installation_id":"install_2","product":"other-app"}`,
		`{"license_id":"license-1","installation_id":"install_2","issuer":"other-issuer"}`,
		`{"license_id":"license-1","installation_id":"install_2","expired":"2099-01-01T00:00:00Z"}`,
		`{"license_id":"license-1","installation_id":"install_2","expired_extra_gracia":"2099-01-01T00:00:00Z"}`,
		`{"license_id":"license-1","license_id":"other","installation_id":"install_2"}`,
		`{"LICENSE_ID":"license-1","installation_id":"install_2"}`,
		valid + `{}`,
	} {
		if _, err := readIssueRequest([]byte(body)); err == nil {
			t.Errorf("accepted invalid request %s", body)
		}
	}
}

func TestSignLicenseV1(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	service := issuer{keyID: "test-key", key: key}
	file, err := service.sign(claims{
		Product:            "example-app",
		LicenseID:          "license-1",
		InstallationID:     "install_2",
		Expired:            time.Date(2027, 12, 31, 0, 0, 0, 123456000, time.UTC),
		ExpiredExtraGracia: time.Date(2028, 1, 7, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	var signed envelope
	if err := json.Unmarshal(file, &signed, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if signed.KeyID != "test-key" {
		t.Fatalf("unexpected key ID %q", signed.KeyID)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":1,"issuer":"phpdevsr-offline-license","product":"example-app","license_id":"license-1","installation_id":"install_2","expired":"2027-12-31T00:00:00.123456Z","expired_extra_gracia":"2028-01-07T00:00:00Z"}`
	if string(payload) != want {
		t.Fatalf("unexpected v1 payload: %s", payload)
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(signed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, payload, signature) {
		t.Fatal("signature does not verify against the raw JSON payload")
	}
}

func TestIssueHTTPErrorResponses(t *testing.T) {
	db, err := openDB("issuer@tcp(127.0.0.1:1)/license_test")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	const token = "test-operator-token"
	app := (issuer{db: db, tokenHash: sha256.Sum256([]byte(token))}).routes()
	for _, tc := range []struct {
		name, token, body string
		status            int
	}{
		{"unauthorized", "wrong", `{}`, http.StatusUnauthorized},
		{"invalid body", token, `{}`, http.StatusBadRequest},
		{"database down", token, `{"license_id":"license-1","installation_id":"install_2"}`, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/licenses/issue", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status || res.Header.Get("Content-Disposition") != "" {
				t.Fatalf("unexpected response: %d, headers %v", res.StatusCode, res.Header)
			}
			if tc.status == http.StatusUnauthorized && res.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing Bearer authentication challenge")
			}
		})
	}
}
