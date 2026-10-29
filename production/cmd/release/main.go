// Offline publisher tooling. Never copy private keys into a distribution.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/DrToppi/gm-vector/production/internal/planner"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Format  string                  `json:"format"`
	Release planner.ReleaseMetadata `json:"release"`
	Files   []entry                 `json:"files"`
}

func read(path string) []byte { b, e := os.ReadFile(path); check(e); return b }
func check(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func writeNew(path string, b []byte) {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	check(e)
	_, e = f.Write(b)
	check(e)
	check(f.Close())
}
func inventory(dir string) ([]entry, error) {
	var files []entry
	root, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("bundle root must be a real directory")
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks forbidden in bundle")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("non-regular bundle file")
		}
		if rel == "signed-manifest.json" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		hash := sha256.New()
		size, err := io.Copy(hash, file)
		if err != nil {
			return err
		}
		files = append(files, entry{rel, size, hex.EncodeToString(hash.Sum(nil))})
		return nil
	})
	if err == nil && len(files) == 0 {
		return nil, errors.New("empty bundle")
	}
	return files, err
}
func main() {
	if len(os.Args) < 2 {
		check(errors.New("usage: release keygen|sign-release|verify-release|sign-bundle|verify-bundle"))
	}
	mode := os.Args[1]
	f := flag.NewFlagSet(mode, flag.ExitOnError)
	dir := f.String("dir", "", "key or bundle directory")
	keyPath := f.String("key", "", "private signing key file (raw Ed25519; never in bundle)")
	keysPath := f.String("keys", "", "trusted publisher keys JSON obtained independently")
	keyID := f.String("key-id", "publisher", "publisher key ID")
	file := f.String("file", "", "signed release metadata")
	out := f.String("out", "", "new output file")
	version := f.String("version", "", "release semantic version")
	date := f.String("date", "", "release date YYYY-MM-DD")
	commit := f.String("commit", "", "exact source commit")
	channel := f.String("channel", "production", "production or synthetic")
	allowSynthetic := f.Bool("allow-synthetic", false, "test verification only; reject synthetic by default")
	check(f.Parse(os.Args[2:]))
	if mode == "keygen" {
		if *dir == "" {
			check(errors.New("--dir required"))
		}
		check(os.MkdirAll(*dir, 0700))
		pub, priv, e := ed25519.GenerateKey(nil)
		check(e)
		keys, e := json.Marshal(map[string]string{*keyID: base64.RawStdEncoding.EncodeToString(pub)})
		check(e)
		writeNew(filepath.Join(*dir, "signing.key"), priv)
		writeNew(filepath.Join(*dir, "public-keys.json"), keys)
		return
	}
	if mode == "sign-release" {
		metadata := planner.ReleaseMetadata{Format: "gma-planner-release-v1", Version: *version, ReleaseDate: *date, GitCommit: *commit, Channel: *channel}
		payload, e := json.Marshal(metadata)
		check(e)
		private := ed25519.PrivateKey(read(*keyPath))
		signed, e := planner.SignDocument(payload, *keyID, private, planner.ReleaseContext)
		check(e)
		_, e = planner.VerifyRelease(signed, map[string]ed25519.PublicKey{*keyID: private.Public().(ed25519.PublicKey)})
		check(e)
		writeNew(*out, signed)
		return
	}
	keys, e := planner.PublisherKeys(read(*keysPath))
	check(e)
	if mode == "verify-release" {
		metadata, e := planner.VerifyRelease(read(*file), keys)
		check(e)
		if metadata.Channel == "synthetic" && !*allowSynthetic {
			check(errors.New("synthetic release not accepted"))
		}
		b, e := json.Marshal(metadata)
		check(e)
		fmt.Println(string(b))
		return
	}
	if mode == "sign-bundle" {
		// Refuse signing with a private key located inside the deliverable tree.
		absoluteDir, e := filepath.Abs(*dir)
		check(e)
		absoluteKey, e := filepath.EvalSymlinks(*keyPath)
		check(e)
		absoluteKey, e = filepath.Abs(absoluteKey)
		check(e)
		rel, e := filepath.Rel(absoluteDir, absoluteKey)
		check(e)
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			check(errors.New("private signing key must be outside bundle"))
		}
		release, e := planner.VerifyRelease(read(*file), keys)
		check(e)
		files, e := inventory(*dir)
		check(e)
		payload, e := json.Marshal(manifest{"gma-planner-bundle-v1", release, files})
		check(e)
		signed, e := planner.SignDocument(payload, *keyID, ed25519.PrivateKey(read(*keyPath)), planner.BundleContext)
		check(e)
		_, e = planner.VerifyDocument(signed, keys, planner.BundleContext)
		check(e)
		writeNew(filepath.Join(*dir, "signed-manifest.json"), signed)
		return
	}
	if mode == "verify-bundle" {
		payload, e := planner.VerifyDocument(read(filepath.Join(*dir, "signed-manifest.json")), keys, planner.BundleContext)
		check(e)
		var m manifest
		check(json.Unmarshal(payload, &m))
		if m.Format != "gma-planner-bundle-v1" || (m.Release.Channel != "production" && !(m.Release.Channel == "synthetic" && *allowSynthetic)) {
			check(errors.New("bundle release not accepted"))
		}
		actual, e := inventory(*dir)
		check(e)
		want, e := json.Marshal(m.Files)
		check(e)
		have, e := json.Marshal(actual)
		check(e)
		if string(want) != string(have) {
			check(errors.New("bundle file inventory/hash mismatch"))
		}
		fmt.Println("Verified signed bundle:", m.Release.Version, m.Release.GitCommit, m.Release.Channel)
		return
	}
	check(errors.New("unknown release operation"))
}
