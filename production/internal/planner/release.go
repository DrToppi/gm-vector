package planner

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

// Set only by the publisher build. No configuration file can replace this root.
var embeddedPublisherKeys string
var embeddedRelease string
var embeddedBuildMode string
var embeddedGitCommit string
var communityVersion = "0.2.0-dev"
var communityDate = "2026-10-29"

const ReleaseContext = "gma-planner-release-v1\x00"
const BundleContext = "gma-planner-bundle-v1\x00"

type SignedDocument struct {
	KeyID     string `json:"key_id"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type ReleaseMetadata struct {
	Format      string `json:"format"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release_date"`
	GitCommit   string `json:"git_commit"`
	Channel     string `json:"channel"`
}

func strictJSON(b []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func PublisherKeys(b []byte) (map[string]ed25519.PublicKey, error) {
	var encoded map[string]string
	if len(b) > 16384 || strictJSON(b, &encoded) != nil || len(encoded) == 0 || len(encoded) > 16 {
		return nil, errors.New("invalid publisher keys")
	}
	keys := make(map[string]ed25519.PublicKey, len(encoded))
	for id, value := range encoded {
		raw, err := base64.RawStdEncoding.DecodeString(value)
		if id == "" || len(id) > 128 || err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, errors.New("invalid publisher key")
		}
		keys[id] = ed25519.PublicKey(raw)
	}
	return keys, nil
}
func VerifyDocument(b []byte, keys map[string]ed25519.PublicKey, context string) ([]byte, error) {
	var signed SignedDocument
	if len(b) > 8*1024*1024 || strictJSON(b, &signed) != nil {
		return nil, errors.New("invalid signed document")
	}
	payload, e1 := base64.RawStdEncoding.DecodeString(signed.Payload)
	sig, e2 := base64.RawStdEncoding.DecodeString(signed.Signature)
	key := keys[signed.KeyID]
	if e1 != nil || e2 != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte(context), payload...), sig) {
		return nil, errors.New("invalid publisher signature")
	}
	return payload, nil
}
func SignDocument(payload []byte, keyID string, key ed25519.PrivateKey, context string) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize || keyID == "" {
		return nil, errors.New("invalid signing key")
	}
	return json.Marshal(SignedDocument{KeyID: keyID, Payload: base64.RawStdEncoding.EncodeToString(payload), Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, append([]byte(context), payload...)))})
}
func VerifyRelease(b []byte, keys map[string]ed25519.PublicKey) (ReleaseMetadata, error) {
	var metadata ReleaseMetadata
	payload, err := VerifyDocument(b, keys, ReleaseContext)
	if err != nil {
		return metadata, err
	}
	if strictJSON(payload, &metadata) != nil || metadata.Format != "gma-planner-release-v1" || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`).MatchString(metadata.Version) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(metadata.GitCommit) || (metadata.Channel != "production" && metadata.Channel != "synthetic") {
		return metadata, errors.New("invalid release metadata")
	}
	if _, err = time.Parse("2006-01-02", metadata.ReleaseDate); err != nil {
		return metadata, errors.New("invalid release date")
	}
	return metadata, nil
}

// RuntimeReleaseTrust describes build provenance. Source builds need no publisher
// keys. If an official signed identity is embedded, it must be internally valid.
func RuntimeReleaseTrust() (map[string]ed25519.PublicKey, ReleaseMetadata, error) {
	var metadata ReleaseMetadata
	if embeddedPublisherKeys == "" && embeddedRelease == "" && (embeddedBuildMode == "" || embeddedBuildMode == "community") {
		commit := embeddedGitCommit
		if commit == "" {
			commit = "unknown"
		}
		return nil, ReleaseMetadata{Format: "gm-vector-release-v1", Version: communityVersion, ReleaseDate: communityDate, GitCommit: commit, Channel: "community"}, nil
	}
	keysRaw, e1 := base64.RawStdEncoding.DecodeString(embeddedPublisherKeys)
	releaseRaw, e2 := base64.RawStdEncoding.DecodeString(embeddedRelease)
	if e1 != nil || e2 != nil {
		return nil, metadata, errors.New("release trust missing")
	}
	keys, err := PublisherKeys(keysRaw)
	if err != nil {
		return nil, metadata, err
	}
	metadata, err = VerifyRelease(releaseRaw, keys)
	if err != nil {
		return nil, metadata, err
	}
	if metadata.GitCommit != embeddedGitCommit || metadata.Channel != embeddedBuildMode {
		return nil, metadata, errors.New("release build identity mismatch")
	}
	return keys, metadata, nil
}
