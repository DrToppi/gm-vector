package planner

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func releaseFixture(t *testing.T, channel string) (map[string]ed25519.PublicKey, ed25519.PrivateKey, []byte, ReleaseMetadata) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := ReleaseMetadata{"gma-planner-release-v1", "1.2.3", "2026-10-28", strings.Repeat("a", 40), channel}
	payload, _ := json.Marshal(metadata)
	signed, err := SignDocument(payload, "publisher", priv, ReleaseContext)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]ed25519.PublicKey{"publisher": pub}, priv, signed, metadata
}
func TestReleaseSignatureAndContext(t *testing.T) {
	keys, _, signed, want := releaseFixture(t, "production")
	got, err := VerifyRelease(signed, keys)
	if err != nil || got != want {
		t.Fatalf("release: %v %+v", err, got)
	}
	var envelope SignedDocument
	json.Unmarshal(signed, &envelope)
	envelope.Payload = base64.RawStdEncoding.EncodeToString([]byte(`{"version":"0.0.1"}`))
	tampered, _ := json.Marshal(envelope)
	if _, err = VerifyRelease(tampered, keys); err == nil {
		t.Fatal("tampered payload trusted")
	}
	foreign, _, _, _ := releaseFixture(t, "production")
	if _, err = VerifyRelease(signed, foreign); err == nil {
		t.Fatal("replacement root trusted")
	}
	if _, err = VerifyDocument(signed, keys, BundleContext); err == nil {
		t.Fatal("cross-context signature trusted")
	}
	if _, err = VerifyRelease(append(signed, []byte(` {}`)...), keys); err == nil {
		t.Fatal("trailing JSON trusted")
	}
}
func TestRuntimeReleaseFailClosedAndSyntheticOptIn(t *testing.T) {
	oldKeys, oldRelease, oldMode, oldCommit := embeddedPublisherKeys, embeddedRelease, embeddedBuildMode, embeddedGitCommit
	t.Cleanup(func() {
		embeddedPublisherKeys, embeddedRelease, embeddedBuildMode, embeddedGitCommit = oldKeys, oldRelease, oldMode, oldCommit
	})
	embeddedPublisherKeys = ""
	embeddedRelease = ""
	embeddedBuildMode = "community"
	if _, meta, err := RuntimeReleaseTrust(); err != nil || meta.Channel != "community" {
		t.Fatal("community source build rejected", err)
	}
	keys, _, signed, metadata := releaseFixture(t, "synthetic")
	encoded, _ := json.Marshal(map[string]string{"publisher": base64.RawStdEncoding.EncodeToString(keys["publisher"])})
	embeddedPublisherKeys = base64.RawStdEncoding.EncodeToString(encoded)
	embeddedRelease = base64.RawStdEncoding.EncodeToString(signed)
	embeddedBuildMode = "synthetic"
	embeddedGitCommit = metadata.GitCommit
	t.Setenv("GMA_ALLOW_SYNTHETIC_BUILD", "")
	if _, _, err := RuntimeReleaseTrust(); err != nil {
		t.Fatal("signed synthetic build rejected", err)
	}
	t.Setenv("GMA_ALLOW_SYNTHETIC_BUILD", "1")
	if _, got, err := RuntimeReleaseTrust(); err != nil || got != metadata {
		t.Fatalf("explicit synthetic: %v", err)
	}
	embeddedGitCommit = strings.Repeat("b", 40)
	if _, _, err := RuntimeReleaseTrust(); err == nil {
		t.Fatal("commit mismatch trusted")
	}
	embeddedGitCommit = metadata.GitCommit
	embeddedBuildMode = "production"
	if _, _, err := RuntimeReleaseTrust(); err == nil {
		t.Fatal("channel mismatch trusted")
	}
}
