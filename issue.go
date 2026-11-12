package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

type issuer struct {
	db        *sql.DB
	keyID     string
	key       ed25519.PrivateKey
	tokenHash [sha256.Size]byte
}

type issueRequest struct {
	LicenseID      string `json:"license_id"`
	InstallationID string `json:"installation_id"`
}

type claims struct {
	Version            int       `json:"version"`
	Issuer             string    `json:"issuer"`
	Product            string    `json:"product"`
	LicenseID          string    `json:"license_id"`
	InstallationID     string    `json:"installation_id"`
	Expired            time.Time `json:"expired"`
	ExpiredExtraGracia time.Time `json:"expired_extra_gracia"`
}

type envelope struct {
	KeyID     string `json:"kid"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

func (i issuer) routes() *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:                    4 * 1024,
		ReadBufferSize:               16 * 1024,
		ReadTimeout:                  10 * time.Second,
		WriteTimeout:                 10 * time.Second,
		IdleTimeout:                  30 * time.Second,
		CaseSensitive:                true,
		StrictRouting:                true,
		DisablePreParseMultipartForm: true,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			// Keep the issuance endpoint's 400 response for an oversized body.
			if errors.Is(err, fiber.ErrRequestEntityTooLarge) {
				err = fiber.NewError(fiber.StatusBadRequest, "Invalid request")
			}
			return fiber.DefaultErrorHandler(c, err)
		},
	})
	app.Post("/v1/licenses/issue", i.issue)
	return app
}

func (i issuer) issue(c fiber.Ctx) error {
	token, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	digest := sha256.Sum256([]byte(token))
	if !ok || subtle.ConstantTimeCompare(digest[:], i.tokenHash[:]) != 1 {
		c.Set(fiber.HeaderWWWAuthenticate, "Bearer")
		return fiber.ErrUnauthorized
	}

	req, err := readIssueRequest(c.BodyRaw())
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()
	var license claims
	var expired, grace string
	err = i.db.QueryRowContext(ctx, `
		SELECT l.license_id, l.installation_id, p.product_id,
		       CAST(l.expired AS CHAR), CAST(l.expired_extra_gracia AS CHAR)
		FROM licenses AS l
		JOIN license_products AS p ON p.product_no = l.product_no
		WHERE l.license_id = ? AND l.installation_id = ?`, req.LicenseID, req.InstallationID).
		Scan(&license.LicenseID, &license.InstallationID, &license.Product, &expired, &grace)
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "License not found")
	}
	if err != nil {
		log.Printf("license database read failed: %v", err)
		return fiber.NewError(fiber.StatusServiceUnavailable, "License database unavailable")
	}

	if license.LicenseID != req.LicenseID || license.InstallationID != req.InstallationID {
		return fiber.NewError(fiber.StatusNotFound, "License not found")
	}
	if !validID(license.Product) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "Invalid license product configuration")
	}
	// Parse the calendar values directly so that invalid MySQL dates cannot be normalized.
	expiry, expiryErr := time.Parse(time.DateTime, expired)
	graceEnd, graceErr := time.Parse(time.DateTime, grace)
	if expiryErr != nil || graceErr != nil || expiry.Year() < 1000 || graceEnd.Year() < 1000 ||
		graceEnd.Before(expiry) || !time.Now().Before(graceEnd) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "Invalid or elapsed license dates")
	}
	license.Expired, license.ExpiredExtraGracia = expiry, graceEnd

	file, err := i.sign(license)
	if err != nil {
		log.Printf("encode license: %v", err)
		return fiber.NewError(fiber.StatusInternalServerError, "License issuance failed")
	}
	log.Printf("license signed product=%s id=%s installation=%s kid=%s expired=%s final_expiry=%s",
		license.Product, license.LicenseID, license.InstallationID, i.keyID, expiry.Format(time.RFC3339Nano), graceEnd.Format(time.RFC3339Nano))
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	c.Set(fiber.HeaderContentDisposition, `attachment; filename=".license"`)
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Send(file)
}

func readIssueRequest(body []byte) (issueRequest, error) {
	var req issueRequest
	if err := json.Unmarshal(body, &req, json.RejectUnknownMembers(true)); err != nil {
		return issueRequest{}, err
	}
	if !validID(req.LicenseID) || !validID(req.InstallationID) {
		return issueRequest{}, errors.New("invalid license or installation ID")
	}
	return req, nil
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (i issuer) sign(c claims) ([]byte, error) {
	c.Version = 1
	c.Issuer = "phpdevsr-offline-license"
	payload, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{
		KeyID:     i.keyID,
		Payload:   base64.RawURLEncoding.EncodeToString(payload),
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(i.key, payload)),
	})
}
