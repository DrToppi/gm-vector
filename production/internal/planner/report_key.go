package planner

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
)

// ReportPublicKey never contains private RSA parameters or an arbitrary display label.
type ReportPublicKey struct {
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
}

func reportKeyID(k ReportPublicKey) string {
	// RFC 7638 canonical RSA public JWK thumbprint.
	raw, _ := json.Marshal(struct {
		E   string `json:"e"`
		Kty string `json:"kty"`
		N   string `json:"n"`
	}{k.E, k.Kty, k.N})
	digest := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
func validateReportPublicKey(k ReportPublicKey) error {
	n, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil || len(n) != 384 || new(big.Int).SetBytes(n).BitLen() != 3072 || n[len(n)-1]&1 == 0 || base64.RawURLEncoding.EncodeToString(n) != k.N || k.Kty != "RSA" || k.E != "AQAB" || k.Alg != "RSA-OAEP-256" || k.Use != "enc" || k.Kid != reportKeyID(k) {
		return errors.New("invalid report public key")
	}
	return nil
}
func loadReportPublicKey(path string) (*ReportPublicKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("report public key unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errors.New("invalid report public key")
	}
	var k ReportPublicKey
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&k) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("invalid report public key")
	}
	if err = validateReportPublicKey(k); err != nil {
		return nil, err
	}
	return &k, nil
}
func (a *App) reportKey(w http.ResponseWriter, r *http.Request, u Principal) error {
	k := a.Config.ReportPublicKey
	if k == nil {
		jsonResponse(w, 200, map[string]any{"configured": false})
		return nil
	}
	if validateReportPublicKey(*k) != nil {
		return appError{503, "report_key_unavailable", "Ключ поддержки недоступен"}
	}
	jsonResponse(w, 200, map[string]any{"configured": true, "key": k})
	return nil
}
