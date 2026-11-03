package planner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

var pwEngines = []string{"chromium", "firefox", "webkit"}
var pwRevisions = map[string]string{"chromium": "1248", "firefox": "1555", "webkit": "2370"}
var pwVersions = map[string]string{"chromium": "156.0.8078.4", "firefox": "157.0", "webkit": "27.2"}
var pwScenarios = []string{"B01", "B02", "B03", "B04", "B05", "B06", "B07", "B08", "B09"}

type pwProcessID struct {
	PID   int
	Start uint64
	UID   uint32
}
type pwProcessInfo struct {
	ID     pwProcessID
	Parent int
}

// Read only same-user process identity/parent metadata, never env or argv.
func readPWProcess(pid int) (pwProcessInfo, bool, error) {
	var out pwProcessInfo
	if pid <= 0 {
		return out, false, errors.New("invalid process identity")
	}
	directory := filepath.Join("/proc", strconv.Itoa(pid))
	info, err := os.Stat(directory)
	if os.IsNotExist(err) {
		return out, false, nil
	}
	if err != nil {
		return out, false, errors.New("ownership observation unavailable")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return out, false, errors.New("ownership metadata unavailable")
	}
	if int(owner.Uid) != os.Getuid() {
		return out, false, nil
	}
	raw, err := os.ReadFile(filepath.Join(directory, "stat"))
	if os.IsNotExist(err) {
		return out, false, nil
	}
	if err != nil || len(raw) > 4096 {
		return out, false, errors.New("process metadata unavailable")
	}
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return out, false, errors.New("invalid process metadata")
	}
	fields := strings.Fields(string(raw)[end+2:])
	if len(fields) < 20 {
		return out, false, errors.New("invalid process metadata")
	}
	parent, e1 := strconv.Atoi(fields[1])
	start, e2 := strconv.ParseUint(fields[19], 10, 64)
	if e1 != nil || e2 != nil || start == 0 {
		return out, false, errors.New("invalid process identity")
	}
	return pwProcessInfo{ID: pwProcessID{pid, start, owner.Uid}, Parent: parent}, true, nil
}

func observePWDescendants(root pwProcessID, seen map[pwProcessID]bool) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return errors.New("ownership observation unavailable")
	}
	current := map[int]pwProcessInfo{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		info, exists, err := readPWProcess(pid)
		if err != nil {
			return err
		}
		if exists {
			current[pid] = info
		}
	}
	owned := map[int]bool{}
	if info, exists := current[root.PID]; exists && info.ID == root {
		owned[root.PID] = true
	}
	for id := range seen {
		if info, exists := current[id.PID]; exists && info.ID == id {
			owned[id.PID] = true
		}
	}
	for change := true; change; {
		change = false
		for pid, info := range current {
			if !owned[pid] && owned[info.Parent] {
				owned[pid], seen[info.ID], change = true, true, true
			}
		}
	}
	return nil
}

func remainingPWProcesses(seen map[pwProcessID]bool) ([]pwProcessID, error) {
	remaining := []pwProcessID{}
	for id := range seen {
		info, exists, err := readPWProcess(id.PID)
		if err != nil {
			return nil, err
		}
		if exists && info.ID == id {
			remaining = append(remaining, id)
		}
	}
	return remaining, nil
}

func forcePWCleanup(identities []pwProcessID) bool {
	forced := false
	for _, id := range identities {
		process, err := os.FindProcess(id.PID)
		if err != nil {
			continue
		}
		info, exists, err := readPWProcess(id.PID)
		if err == nil && exists && info.ID == id {
			forced = true
			_ = process.Kill()
		}
		_ = process.Release()
	}
	return forced
}

type pwReady struct {
	Format        string `json:"format"`
	SchemaVersion int    `json:"schema_version"`
	Nonce         string `json:"run_nonce"`
	Engine        string `json:"engine"`
	Playwright    string `json:"playwright_version"`
	Revision      string `json:"browser_revision"`
	Browser       string `json:"browser_version"`
	WorkerPID     int    `json:"worker_pid"`
	Stage         string `json:"stage"`
}

func validatePWReady(raw []byte, engine, nonce string) (pwReady, error) {
	var ready pwReady
	fail := errors.New("invalid lifecycle readiness")
	if len(raw) == 0 || len(raw) > 4096 || !uniquePWJSON(raw) {
		return ready, fail
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ready) != nil || ready.Format != "gm-vector-browser-ready" || ready.SchemaVersion != 1 || ready.Nonce != nonce || ready.Engine != engine || ready.Playwright != "1.64.1" || pwRevisions[engine] == "" || ready.Revision != pwRevisions[engine] || ready.Browser != pwVersions[engine] || ready.WorkerPID <= 0 || ready.Stage != "owned-page-created" {
		return ready, fail
	}
	return ready, nil
}

func ownedPWReady(root pwProcessID, ready pwReady, seen map[pwProcessID]bool) bool {
	worker, exists, err := readPWProcess(ready.WorkerPID)
	live, liveErr := remainingPWProcesses(seen)
	return err == nil && liveErr == nil && exists && ready.WorkerPID != root.PID && seen[worker.ID] && len(live) >= 2
}

func readPWPrivate(file string, limit int64) ([]byte, bool, error) {
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > limit {
		return nil, true, errors.New("invalid private lifecycle artifact")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Getuid() {
		return nil, true, errors.New("invalid lifecycle artifact owner")
	}
	raw, err := os.ReadFile(file)
	if err != nil || int64(len(raw)) > limit {
		return nil, true, errors.New("private lifecycle artifact unavailable")
	}
	return raw, true, nil
}

func pwCommand(ctx context.Context, node, web, engine string, env []string, output io.Writer) *exec.Cmd {
	cmd := exec.CommandContext(ctx, node, filepath.Join(web, "node_modules/@playwright/test/cli.js"), "test", "--config=playwright.config.ts", "--project="+engine)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = web, env, output, output
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

type pwLifecycle struct {
	Ready               bool
	Signal              string
	Observed, Remaining int
	Forced              bool
	Reason              string
}

func passedPWLifecycle(out pwLifecycle) bool {
	return out.Reason == "" && out.Ready && out.Signal == "SIGINT" && out.Observed >= 2 && out.Remaining == 0 && !out.Forced
}

func runPWLifecycle(parent context.Context, node, web, engine, runDirectory, nonce string, input []byte, environment []string) (out pwLifecycle) {
	out.Signal = "none"
	directory, err := os.MkdirTemp(runDirectory, "lifecycle-")
	if err != nil {
		out.Reason = "private_directory"
		return
	}
	var private *os.File
	defer func() {
		if private != nil {
			_ = private.Close()
		}
		if out.Reason != "" && retainPWFailure(os.Getenv("GMA_PW_DEBUG_DIRECTORY"), directory, engine) != nil {
			out.Reason += "+debug_copy_failed"
		}
		_ = os.RemoveAll(directory)
	}()
	file := filepath.Join(directory, "fixture.json")
	ipc, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		out.Reason = "private_input"
		return
	}
	_, writeErr := ipc.Write(input)
	closeErr := ipc.Close()
	if writeErr != nil || closeErr != nil {
		out.Reason = "private_input"
		return
	}
	private, err = os.OpenFile(filepath.Join(directory, "process.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		out.Reason = "private_log"
		return
	}
	log := &pwBoundedLog{writer: private, remaining: 1048576}
	env := []string{}
	for _, value := range environment {
		if !strings.HasPrefix(value, "GMA_PW_FIXTURE_FILE=") && !strings.HasPrefix(value, "GMA_PW_LIFECYCLE_PROBE=") {
			env = append(env, value)
		}
	}
	env = append(env, "GMA_PW_FIXTURE_FILE="+file, "GMA_PW_LIFECYCLE_PROBE=1")
	probe, stopProbe := context.WithTimeout(parent, 15*time.Second)
	defer stopProbe()
	child, cancelChild := context.WithCancel(probe)
	defer cancelChild()
	cmd := pwCommand(child, node, web, engine, env, log)
	var signaled atomic.Bool
	signal := cmd.Cancel
	cmd.Cancel = func() error {
		err := signal()
		if err == nil {
			signaled.Store(true)
		}
		return err
	}
	if cmd.Start() != nil {
		out.Reason = "cli_start"
		return
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	finished := false
	checkExit := func() bool {
		if !finished {
			select {
			case <-done:
				finished = true
			default:
			}
		}
		return finished
	}
	root, rootExists, identityErr := readPWProcess(cmd.Process.Pid)
	seen := map[pwProcessID]bool{}
	if identityErr != nil || !rootExists {
		out.Reason = "ownership_unavailable"
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for out.Reason == "" && !out.Ready {
		if checkExit() {
			out.Reason = "spontaneous_exit"
			break
		}
		if observePWDescendants(root.ID, seen) != nil {
			out.Reason = "ownership_unavailable"
			break
		}
		raw, exists, err := readPWPrivate(filepath.Join(directory, "cancellation-ready.json"), 4096)
		if err != nil {
			out.Reason = "invalid_readiness"
			break
		}
		if exists {
			ready, err := validatePWReady(raw, engine, nonce)
			if err != nil || observePWDescendants(root.ID, seen) != nil {
				out.Reason = "invalid_readiness"
				break
			}
			if !ownedPWReady(root.ID, ready, seen) {
				out.Reason = "empty_or_unowned_readiness"
				break
			}
			if checkExit() {
				out.Reason = "spontaneous_exit"
				break
			}
			if probe.Err() != nil {
				out.Reason = "readiness_deadline"
				break
			}
			out.Ready = true
			break
		}
		select {
		case <-probe.Done():
			out.Reason = "readiness_deadline"
		case <-done:
			finished = true
			out.Reason = "spontaneous_exit"
		case <-tick.C:
		}
	}
	out.Observed = len(seen)
	// This is the same CommandContext -> Cancel -> SIGINT path as the journey.
	// A readiness failure still requests cleanup, but can never become success.
	requested := out.Ready && out.Reason == "" && probe.Err() == nil
	cancelChild()
	deadline := time.Now().Add(5 * time.Second)
	if absolute, ok := probe.Deadline(); ok && absolute.Before(deadline) {
		deadline = absolute
	}
	remaining := []pwProcessID{}
	for {
		checkExit()
		if rootExists && observePWDescendants(root.ID, seen) != nil && out.Reason == "" {
			out.Reason = "ownership_unavailable"
		}
		var err error
		remaining, err = remainingPWProcesses(seen)
		if err != nil && out.Reason == "" {
			out.Reason = "ownership_unavailable"
		}
		if finished && len(remaining) == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		if finished {
			<-tick.C
		} else {
			select {
			case <-done:
				finished = true
			case <-tick.C:
			}
		}
	}
	if !finished || len(remaining) > 0 {
		if out.Reason == "" {
			out.Reason = "cleanup_deadline"
		}
		owned := append([]pwProcessID{}, remaining...)
		if rootExists {
			owned = append(owned, root.ID)
		}
		out.Forced = forcePWCleanup(owned)
		if !finished {
			select {
			case <-done:
				finished = true
			case <-time.After(5 * time.Second):
			}
		}
	}
	remaining, err = remainingPWProcesses(seen)
	if err != nil && out.Reason == "" {
		out.Reason = "ownership_unavailable"
	}
	out.Remaining = len(remaining)
	out.Observed = len(seen)
	if signaled.Load() {
		out.Signal = "SIGINT"
	}
	if out.Reason == "" && (!requested || !out.Ready || out.Observed < 2 || out.Signal != "SIGINT" || !finished || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 130 || out.Remaining != 0 || out.Forced || log.truncated) {
		out.Reason = "invalid_cancellation_order_or_exit"
	}
	if out.Reason == "" {
		raw, exists, err := readPWPrivate(filepath.Join(directory, "receipt.json"), 262144)
		var receipt pwReceipt
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err != nil || !exists || !uniquePWJSON(raw) || decoder.Decode(&receipt) != nil || receipt.Engine != engine || receipt.Nonce != nonce || receipt.Status != "interrupted" || receipt.TestCount != 1 || len(receipt.Scenarios) != 0 || receipt.Playwright != "1.64.1" || receipt.Revision != pwRevisions[engine] || receipt.Browser != pwVersions[engine] {
			out.Reason = "invalid_interrupted_receipt"
		}
	}
	if absolute, ok := probe.Deadline(); out.Reason == "" && (probe.Err() != nil || ok && !time.Now().Before(absolute)) {
		out.Reason = "probe_deadline"
	}
	return
}

func validatePWDebugDirectory(directory string) error {
	fail := errors.New("invalid private browser debug directory")
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return fail
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return fail
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fail
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Getuid() {
		return fail
	}
	return nil
}

func retainPWFailure(directory, runDirectory, engine string) error {
	if directory == "" {
		return nil
	}
	if validatePWDebugDirectory(directory) != nil || pwRevisions[engine] == "" {
		return errors.New("invalid private browser debug destination")
	}
	for name, limit := range map[string]int64{"process.log": 1048576, "receipt.json": 262144} {
		source := filepath.Join(runDirectory, name)
		info, err := os.Lstat(source)
		if os.IsNotExist(err) && name == "receipt.json" {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > limit {
			return errors.New("invalid private browser diagnostic source")
		}
		input, err := os.Open(source)
		if err != nil {
			return errors.New("private browser diagnostic unavailable")
		}
		output, err := os.OpenFile(filepath.Join(directory, engine+"."+name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return errors.New("private browser diagnostic destination unavailable")
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
		closeErr := output.Close()
		input.Close()
		if copyErr != nil || closeErr != nil || written > limit {
			_ = os.Remove(filepath.Join(directory, engine+"."+name))
			return errors.New("private browser diagnostic copy failed")
		}
	}
	return nil
}

// Call logs may include credentials, so they remain private and bounded. The
// child can finish writing, but crossing the bound blocks acceptance.
type pwBoundedLog struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int
	truncated bool
}

func (log *pwBoundedLog) Write(value []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	keep := len(value)
	if keep > log.remaining {
		keep = log.remaining
		log.truncated = true
	}
	if keep > 0 {
		if _, err := log.writer.Write(value[:keep]); err != nil {
			return 0, err
		}
		log.remaining -= keep
	}
	return len(value), nil
}

type pwScenario struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
}
type pwReceipt struct {
	FailurePhase          string       `json:"failure_phase,omitempty"`
	FailureSite           string       `json:"failure_site,omitempty"`
	FailureClass          string       `json:"failure_class,omitempty"`
	Format                string       `json:"format"`
	SchemaVersion         int          `json:"schema_version"`
	Nonce                 string       `json:"run_nonce"`
	Engine                string       `json:"engine"`
	Playwright            string       `json:"playwright_version"`
	Revision              string       `json:"browser_revision"`
	Browser               string       `json:"browser_version"`
	Status                string       `json:"status"`
	TestCount             int          `json:"test_count"`
	Scenarios             []pwScenario `json:"scenarios"`
	FunctionalTLS         string       `json:"functional_tls"`
	StrictTLS             string       `json:"strict_tls"`
	NativeClipboard       string       `json:"native_clipboard"`
	ClipboardUI           string       `json:"clipboard_ui"`
	CertificateRejections []string     `json:"certificate_rejections"`
}

func validatePWReceipt(raw []byte, engine, nonce string) error {
	fail := errors.New("invalid browser receipt")
	if len(raw) == 0 || len(raw) > 262144 {
		return fail
	}
	var receipt pwReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !uniquePWJSON(raw) || decoder.Decode(&receipt) != nil {
		return fail
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return fail
	}
	if receipt.FailurePhase != "" || receipt.FailureSite != "" || receipt.FailureClass != "" {
		return fail
	}
	if receipt.Format != "gm-vector-browser-receipt" || receipt.SchemaVersion != 1 || receipt.Engine != engine || pwRevisions[engine] == "" || receipt.Nonce != nonce || receipt.Playwright != "1.64.1" || receipt.Revision != pwRevisions[engine] || receipt.Browser != pwVersions[engine] || receipt.Status != "passed" || receipt.TestCount != 1 || len(receipt.Scenarios) != 9 {
		return fail
	}
	if receipt.FunctionalTLS != "owned-loopback-self-signed-exception" || receipt.StrictTLS != "unknown-ca-and-mismatch-rejected-not-isolated-hostname-proof" {
		return fail
	}
	if len(receipt.CertificateRejections) != 2 || receipt.CertificateRejections[0] != engine+"-certificate-rejected" || receipt.CertificateRejections[1] != engine+"-certificate-rejected" {
		return fail
	}
	if (receipt.NativeClipboard != "write-succeeded" && receipt.NativeClipboard != "write-denied" && receipt.NativeClipboard != "api-unavailable") || receipt.ClipboardUI != "controlled-success-and-denial" {
		return fail
	}
	for i, item := range receipt.Scenarios {
		if item.ID != pwScenarios[i] || item.Status != "passed" || item.Attempts != 1 {
			return fail
		}
	}
	return nil
}
func uniquePWJSON(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		item, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, container := item.(json.Delim)
		if !container {
			return true
		}
		if delimiter == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
		} else if delimiter == '[' {
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
		} else {
			return false
		}
		_, err = decoder.Token()
		return err == nil
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
func TestPlaywrightHarnessValidation(t *testing.T) {
	t.Run("private_debug", func(t *testing.T) {
		directory := t.TempDir()
		if os.Chmod(directory, 0700) != nil || validatePWDebugDirectory(directory) != nil {
			t.Fatal("private debug directory rejected")
		}
		if validatePWDebugDirectory("relative") == nil {
			t.Fatal("relative debug directory admitted")
		}
		if os.Chmod(directory, 0755) != nil || validatePWDebugDirectory(directory) == nil {
			t.Fatal("public debug directory admitted")
		}
		os.Chmod(directory, 0700)
		link := filepath.Join(t.TempDir(), "link")
		if os.Symlink(directory, link) != nil || validatePWDebugDirectory(link) == nil {
			t.Fatal("symlinked debug directory admitted")
		}
		run := t.TempDir()
		if os.WriteFile(filepath.Join(run, "process.log"), []byte("private synthetic diagnostic"), 0600) != nil || os.WriteFile(filepath.Join(run, "fixture.json"), []byte("must not be copied"), 0600) != nil {
			t.Fatal("debug unit fixture unavailable")
		}
		if retainPWFailure(directory, run, "chromium") != nil {
			t.Fatal("private failure retention failed")
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 1 || entries[0].Name() != "chromium.process.log" {
			t.Fatal("unexpected debug artifact retained")
		}
		info, err := os.Stat(filepath.Join(directory, "chromium.process.log"))
		if err != nil || info.Mode().Perm() != 0600 || retainPWFailure(directory, run, "chromium") == nil {
			t.Fatal("private exclusive diagnostic guard failed")
		}
		if os.WriteFile(filepath.Join(run, "process.log"), bytes.Repeat([]byte("x"), 1048577), 0600) != nil || retainPWFailure(directory, run, "firefox") == nil {
			t.Fatal("oversized debug source admitted")
		}
	})
	var bounded bytes.Buffer
	log := &pwBoundedLog{writer: &bounded, remaining: 4}
	if n, err := log.Write([]byte("abcdef")); n != 6 || err != nil || !log.truncated || bounded.String() != "abcd" {
		t.Fatal("private log bound failed")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node24 required for IPC contract unit")
	}
	if exec.Command(node, "../../web/browser/contracts.unit.cjs").Run() != nil {
		t.Fatal("private IPC/reporter unit failed")
	}
	t.Run("lifecycle_readiness", func(t *testing.T) {
		good := pwReady{Format: "gm-vector-browser-ready", SchemaVersion: 1, Nonce: "synthetic", Engine: "chromium", Playwright: "1.64.1", Revision: "1248", Browser: "156.0.8078.4", WorkerPID: os.Getpid(), Stage: "owned-page-created"}
		encode := func(value pwReady) []byte { raw, _ := json.Marshal(value); return raw }
		if _, err := validatePWReady(encode(good), "chromium", "synthetic"); err != nil {
			t.Fatal("valid readiness rejected")
		}
		for _, mutate := range []func(*pwReady){
			func(v *pwReady) { v.Nonce = "other" }, func(v *pwReady) { v.Engine = "webkit" },
			func(v *pwReady) { v.Revision = "old" }, func(v *pwReady) { v.WorkerPID = 0 },
			func(v *pwReady) { v.Stage = "before-page" }, func(v *pwReady) { v.Browser = "1.0" },
		} {
			invalid := good
			mutate(&invalid)
			if _, err := validatePWReady(encode(invalid), "chromium", "synthetic"); err == nil {
				t.Fatal("invalid readiness admitted")
			}
		}
		for _, invalid := range [][]byte{nil, []byte("{}"), bytes.Repeat([]byte("x"), 4097), bytes.Replace(encode(good), []byte(`"schema_version":1`), []byte(`"schema_version":2,"schema_version":1`), 1), bytes.Replace(encode(good), []byte(`"schema_version":1`), []byte(`"extra":1,"schema_version":1`), 1)} {
			if _, err := validatePWReady(invalid, "chromium", "synthetic"); err == nil {
				t.Fatal("malformed readiness admitted")
			}
		}
		file := filepath.Join(t.TempDir(), "ready.json")
		if os.WriteFile(file, encode(good), 0600) != nil {
			t.Fatal("private readiness fixture unavailable")
		}
		if _, exists, err := readPWPrivate(file, 4096); err != nil || !exists {
			t.Fatal("private readiness rejected")
		}
		if os.Chmod(file, 0644) != nil {
			t.Fatal("private readiness control unavailable")
		}
		if _, _, err := readPWPrivate(file, 4096); err == nil {
			t.Fatal("public readiness admitted")
		}
		valid := pwLifecycle{Ready: true, Signal: "SIGINT", Observed: 2}
		if !passedPWLifecycle(valid) {
			t.Fatal("valid lifecycle rejected")
		}
		for _, mutate := range []func(*pwLifecycle){func(v *pwLifecycle) { v.Ready = false }, func(v *pwLifecycle) { v.Signal = "none" }, func(v *pwLifecycle) { v.Observed = 0 }, func(v *pwLifecycle) { v.Remaining = 1 }, func(v *pwLifecycle) { v.Forced = true }, func(v *pwLifecycle) { v.Reason = "spontaneous_exit" }} {
			invalid := valid
			mutate(&invalid)
			if passedPWLifecycle(invalid) {
				t.Fatal("failed lifecycle admitted")
			}
		}
	})
	t.Run("owned_process_observation", func(t *testing.T) {
		root, exists, err := readPWProcess(os.Getpid())
		if err != nil || !exists {
			t.Fatal("reference ownership observation unavailable")
		}
		if ownedPWReady(root.ID, pwReady{WorkerPID: os.Getpid()}, map[pwProcessID]bool{}) {
			t.Fatal("empty ownership observation admitted")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Admission control only; real acceptance uses the actual CLI/worker/page.
		child := exec.CommandContext(ctx, node, "-e", "setInterval(()=>{},1000)")
		child.Stdout, child.Stderr = io.Discard, io.Discard
		if child.Start() != nil {
			t.Fatal("owned observation control unavailable")
		}
		waited := false
		defer func() {
			if !waited {
				_ = child.Process.Kill()
				_ = child.Wait()
			}
		}()
		identity, exists, err := readPWProcess(child.Process.Pid)
		if err != nil || !exists {
			t.Fatal("owned control identity unavailable")
		}
		seen := map[pwProcessID]bool{}
		if observePWDescendants(root.ID, seen) != nil || !seen[identity.ID] {
			t.Fatal("owned descendant was not observed")
		}
		wrong := identity.ID
		wrong.Start++
		if forcePWCleanup([]pwProcessID{wrong}) {
			t.Fatal("reused process identity was signaled")
		}
		wrong = identity.ID
		wrong.UID++
		if forcePWCleanup([]pwProcessID{wrong}) {
			t.Fatal("different owner identity was signaled")
		}
		live, exists, err := readPWProcess(identity.ID.PID)
		if err != nil || !exists || live.ID != identity.ID {
			t.Fatal("identity negative controls changed owned process")
		}
		if !forcePWCleanup([]pwProcessID{identity.ID}) {
			t.Fatal("owned fallback control did not signal")
		}
		_ = child.Wait()
		waited = true
		remaining, err := remainingPWProcesses(map[pwProcessID]bool{identity.ID: true})
		if err != nil || len(remaining) != 0 {
			t.Fatal("owned control was not reaped")
		}
	})

	good := pwReceipt{Format: "gm-vector-browser-receipt", SchemaVersion: 1, Nonce: "synthetic", Engine: "chromium", Playwright: "1.64.1", Revision: "1248", Browser: "156.0.8078.4", Status: "passed", TestCount: 1, FunctionalTLS: "owned-loopback-self-signed-exception", StrictTLS: "unknown-ca-and-mismatch-rejected-not-isolated-hostname-proof", NativeClipboard: "write-succeeded", ClipboardUI: "controlled-success-and-denial"}
	good.CertificateRejections = []string{"chromium-certificate-rejected", "chromium-certificate-rejected"}
	for _, id := range pwScenarios {
		good.Scenarios = append(good.Scenarios, pwScenario{id, "passed", 1})
	}
	encode := func(value pwReceipt) []byte { raw, _ := json.Marshal(value); return raw }
	if validatePWReceipt(encode(good), "chromium", "synthetic") != nil {
		t.Fatal("valid receipt rejected")
	}
	mutations := map[string]func(*pwReceipt){"wrong_nonce": func(v *pwReceipt) { v.Nonce = "other" }, "wrong_engine": func(v *pwReceipt) { v.Engine = "firefox" }, "wrong_revision": func(v *pwReceipt) { v.Revision = "old" }, "wrong_version": func(v *pwReceipt) { v.Playwright = "1.0" }, "zero_tests": func(v *pwReceipt) { v.TestCount = 0 }, "extra_tests": func(v *pwReceipt) { v.TestCount = 2 }, "failed": func(v *pwReceipt) { v.Status = "failed" }, "missing": func(v *pwReceipt) { v.Scenarios = v.Scenarios[:8] }, "duplicate": func(v *pwReceipt) { v.Scenarios[1].ID = "B01" }, "skipped": func(v *pwReceipt) { v.Scenarios[0].Status = "skipped" }, "retried": func(v *pwReceipt) { v.Scenarios[0].Attempts = 2 }, "empty_browser": func(v *pwReceipt) { v.Browser = "" }, "tls_claim": func(v *pwReceipt) { v.FunctionalTLS = "trusted" }}
	mutations["missing_tls_result"] = func(v *pwReceipt) { v.CertificateRejections = v.CertificateRejections[:1] }
	mutations["transport_as_tls"] = func(v *pwReceipt) { v.CertificateRejections[0] = "connection-refused" }
	mutations["missing_native_clipboard"] = func(v *pwReceipt) { v.NativeClipboard = "" }
	mutations["false_clipboard_claim"] = func(v *pwReceipt) { v.ClipboardUI = "native-success-and-denial" }
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := good
			v.Scenarios = append([]pwScenario{}, good.Scenarios...)
			v.CertificateRejections = append([]string{}, good.CertificateRejections...)
			mutate(&v)
			if validatePWReceipt(encode(v), "chromium", "synthetic") == nil {
				t.Fatal("invalid receipt admitted")
			}
		})
	}
	for _, raw := range [][]byte{nil, []byte("{}"), append(encode(good), []byte(" {}")...), bytes.Repeat([]byte("x"), 262145), bytes.Replace(encode(good), []byte(`"schema_version":1`), []byte(`"unexpected":1,"schema_version":1`), 1), bytes.Replace(encode(good), []byte(`"schema_version":1`), []byte(`"schema_version":2,"schema_version":1`), 1)} {
		if validatePWReceipt(raw, "chromium", "synthetic") == nil {
			t.Fatal("malformed receipt admitted")
		}
	}
}

// A real API regression for the member preview/apply path observed in B06.
// It never attaches to an installed application or prints response bodies.
func TestPlaywrightMemberMilestoneRegression(t *testing.T) {
	for _, state := range []string{"ready", "blocked_capacity"} {
		t.Run(state, func(t *testing.T) { testPWMemberMilestone(t, state) })
	}
}

func testPWMemberMilestone(t *testing.T, state string) {
	f := newReadPathFixture(t, 1)
	ctx := context.Background()
	project := f.projects[0]
	project.Brief = "PW protected manager brief"
	project.People = project.People[:1]
	if state == "blocked_capacity" {
		project.People[0].Week = map[string]Activity{"1": {Active: false}}
	}
	project.Links = []Link{}
	project.Tasks = []Task{
		{ID: "pw-milestone", Title: "PW milestone", Status: "todo", Responsibility: "internal", Assignments: []Assignment{}},
		{ID: "pw-owned-leaf", Parent: "pw-milestone", Title: "PW owned leaf", Status: "todo", Responsibility: "internal", RequestedStart: project.Start, Planned: 192, Assignments: []Assignment{{EmployeeID: "profile-employee-00", Daily: 96}}},
		{ID: "pw-unowned-leaf", Title: "PW unowned leaf", Status: "todo", Responsibility: "customer", RequestedStart: project.Start, Start: project.Start, End: project.Start, DurationDays: 1, Assignments: []Assignment{}},
	}
	if _, err := f.store.DB.Exec(ctx, "UPDATE employees SET user_id='profile-user-01' WHERE id='profile-employee-00'"); err != nil {
		t.Fatal("member employee binding unavailable")
	}
	if _, err := f.store.DB.Exec(ctx, "UPDATE memberships SET role='member' WHERE project_id=$1 AND user_id='profile-user-01'", project.ID); err != nil {
		t.Fatal("member role fixture unavailable")
	}
	server := httptest.NewUnstartedServer(f.app.Handler())
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	serverURL, _ := url.Parse(server.URL)
	call := func(method, path string, body any, user int) (int, []byte) {
		t.Helper()
		request := f.request(method, path, body, "")
		request.URL.Scheme, request.URL.Host = serverURL.Scheme, serverURL.Host
		request.RequestURI = ""
		request.Header.Set("Cookie", "__Host-gma="+f.sessions[user].cookie)
		request.Header.Set("X-CSRF-Token", f.sessions[user].csrf)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("member regression transport failed")
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
		if err != nil || len(raw) > 1048576 {
			t.Fatal("member regression bounded response failed")
		}
		return response.StatusCode, raw
	}
	decode := func(raw []byte, target any) {
		t.Helper()
		if json.Unmarshal(raw, target) != nil {
			t.Fatal("member regression response schema failed")
		}
	}
	status, raw := call("PUT", "/projects/"+project.ID, saveRequest{Project: snapshotProject(project)}, 0)
	if status != 200 {
		t.Fatalf("member regression manager setup HTTP%d", status)
	}
	var stored Project
	decode(raw, &stored)
	if !stored.Tasks[0].Summary || stored.Tasks[0].Status != "todo" || len(stored.Tasks[0].Assignments) != 0 {
		t.Fatal("member regression summary precondition failed")
	}
	status, raw = call("GET", "/auth/me", nil, 1)
	var me struct {
		User User `json:"user"`
	}
	decode(raw, &me)
	if status != 200 || me.User.Admin || me.User.EmployeeID != "profile-employee-00" {
		t.Fatal("member regression principal precondition failed")
	}
	status, raw = call("GET", "/projects/"+project.ID, nil, 1)
	var envelope struct {
		Role    string  `json:"role"`
		Project Project `json:"project"`
	}
	decode(raw, &envelope)
	if status != 200 || envelope.Role != "member" {
		t.Fatal("member regression project role precondition failed")
	}
	stored = envelope.Project
	if stored.CalculationState != state {
		t.Fatal("member regression calculation state precondition failed")
	}
	var desired Project
	cloned, _ := json.Marshal(stored)
	decode(cloned, &desired)
	desired.Tasks[1].Status = "doing"
	desired.Tasks[1].Brief = "PW member brief"
	status, raw = call("POST", "/projects/"+project.ID+"/preview", saveRequest{Project: snapshotProject(desired)}, 1)
	if status != 200 {
		t.Fatalf("owned leaf preview HTTP%d, expected200", status)
	}
	var preview Project
	decode(raw, &preview)
	desired.Tasks[0].Status = "doing"
	if !reflect.DeepEqual(snapshotProject(desired), snapshotProject(preview)) {
		t.Fatal("preview changed fields outside owned status/brief and derived milestone status")
	}
	status, raw = call("PUT", "/projects/"+project.ID, saveRequest{Project: snapshotProject(preview)}, 1)
	code := "none"
	if status != 200 {
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		decode(raw, &failure)
		code = "unexpected"
		if failure.Error.Code == "forbidden" {
			code = "forbidden"
		}
	}
	t.Logf("PW_MEMBER_MILESTONE preview_http=200 apply_http=%d error=%s canonical_changes=owned_leaf.status,owned_leaf.brief,derived_milestone.status protected_fields_unchanged=true", status, code)
	if status != 200 {
		t.Fatalf("owned leaf preview/apply must succeed: HTTP%d code=%s, expected200", status, code)
	}
	var saved Project
	decode(raw, &saved)
	desired.Revision++
	if !reflect.DeepEqual(snapshotProject(desired), snapshotProject(saved)) {
		t.Fatal("member preview apply changed protected fields")
	}
	clone := func(project Project) Project {
		raw, _ := json.Marshal(project)
		var copied Project
		decode(raw, &copied)
		return copied
	}
	forged := clone(saved)
	forged.Tasks[0].Status = "done"
	status, raw = call("PUT", "/projects/"+project.ID, saveRequest{Project: snapshotProject(forged)}, 1)
	if status != 200 {
		t.Fatalf("derived-only milestone transport HTTP%d, expected200", status)
	}
	var normalized Project
	decode(raw, &normalized)
	saved.Revision++
	if normalized.CalculationState != state || normalized.Tasks[0].Status != "doing" || !reflect.DeepEqual(snapshotProject(saved), snapshotProject(normalized)) {
		t.Fatal("forged milestone status persisted or protected fields changed")
	}
	storedRaw, err := readProject(ctx, f.store.DB, project.ID)
	if err != nil || storedRaw.Tasks[0].Status != "doing" {
		t.Fatal("forged milestone status reached storage")
	}
	for _, control := range []struct {
		name   string
		mutate func(*Project)
	}{
		{"parent_title", func(p *Project) { p.Tasks[0].Title = "Forbidden parent title" }},
		{"parent_brief", func(p *Project) { p.Tasks[0].Brief = "Forbidden parent brief" }},
		{"parent_plan", func(p *Project) { p.Tasks[0].Planned++ }},
		{"forged_summary_leaf", func(p *Project) { p.Tasks[2].Status = "doing"; p.Tasks[2].Summary = true }},
	} {
		t.Run(control.name, func(t *testing.T) {
			attempt := clone(normalized)
			control.mutate(&attempt)
			status, raw := call("PUT", "/projects/"+project.ID, saveRequest{Project: snapshotProject(attempt)}, 1)
			var failure struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			decode(raw, &failure)
			if status != 403 || failure.Error.Code != "forbidden" {
				t.Fatal("protected member mutation was not forbidden")
			}
			status, raw = call("GET", "/projects/"+project.ID, nil, 1)
			var after struct {
				Project Project `json:"project"`
			}
			decode(raw, &after)
			if status != 200 || !reflect.DeepEqual(snapshotProject(normalized), snapshotProject(after.Project)) {
				t.Fatal("forbidden member mutation changed project state")
			}
		})
	}
	t.Log("PW_MEMBER_MILESTONE forged_parent_done_not_persisted=true protected_parent_fields=forbidden forged_summary_leaf=forbidden unchanged_after_denial=true state=" + state)
}

// The three engines always run; a manually filtered Go child cannot become a
// passing full root. This fixture never attaches to an installation or browser.
func TestPlaywrightE2E(t *testing.T) {
	if os.Getenv("GMA_PLAYWRIGHT_E2E") != "1" {
		t.Skip("set GMA_PLAYWRIGHT_E2E=1 for isolated three-engine E2E")
	}
	if directory := os.Getenv("GMA_PW_DEBUG_DIRECTORY"); directory != "" && validatePWDebugDirectory(directory) != nil {
		t.Fatal("explicit private browser debug directory refused")
	}
	dsn, err := url.Parse(os.Getenv("GMA_TEST_DSN"))
	if err != nil || dsn.Path != "/gma_planner_test" || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		t.Fatal("explicit disposable database required")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node24 required")
	}
	web, err := filepath.Abs("../../web")
	if err != nil {
		t.Fatal("web fixture unavailable")
	}
	for _, file := range []string{"dist/index.html", "node_modules/@playwright/test/cli.js", "node_modules/playwright-core/browsers.json"} {
		if _, e := os.Stat(filepath.Join(web, file)); e != nil {
			t.Fatal("build and pinned Playwright prerequisites required")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 570*time.Second)
	defer cancel()
	completed := 0
	for _, engine := range pwEngines {
		t.Run(engine, func(t *testing.T) { completed++; runPWEngine(t, ctx, node, web, dsn.String(), engine) })
	}
	if completed != 3 {
		t.Fatal("all three required engines must execute")
	}
}
func runPWEngine(t *testing.T, parent context.Context, node, web, dsn, engine string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 180*time.Second)
	defer cancel()
	root, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer root.DB.Close()
	schema := "pw_" + newID()
	if _, err = root.DB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("isolated schema unavailable")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, e := root.DB.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error("schema cleanup failed")
		}
	}()
	parsed, _ := url.Parse(dsn)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal("isolated store unavailable")
	}
	defer store.DB.Close()
	if store.Migrate(ctx) != nil {
		t.Fatal("fixture migration failed")
	}
	server := httptest.NewUnstartedServer(nil)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	defer server.Close()
	runDir := t.TempDir()
	if os.Chmod(runDir, 0700) != nil {
		t.Fatal("private directory unavailable")
	}
	support, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal("synthetic support key unavailable")
	}
	modulus := base64.RawURLEncoding.EncodeToString(support.N.Bytes())
	thumb := sha256.Sum256([]byte(`{"e":"AQAB","kty":"RSA","n":"` + modulus + `"}`))
	cfg := Config{Origin: "https://" + server.Listener.Addr().String(), WebDir: filepath.Join(web, "dist"), Secret: random(32), PlanningTimezone: "America/New_York", ReportDirectory: filepath.Join(runDir, "reports"), ReportPublicKey: &ReportPublicKey{Kty: "RSA", N: modulus, E: "AQAB", Alg: "RSA-OAEP-256", Use: "enc", Kid: base64.RawURLEncoding.EncodeToString(thumb[:])}}
	if os.Mkdir(cfg.ReportDirectory, 0700) != nil {
		t.Fatal("private report fixture unavailable")
	}
	app, err := NewApp(store, cfg)
	if err != nil {
		t.Fatal("app fixture unavailable")
	}
	nonce := token()
	// A second exact loopback fixture has no IP SAN. Its DNS name is used only
	// by the private ownership proof; strict browser access uses its IP origin.
	mismatchKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("synthetic mismatch key unavailable")
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"playwright-mismatch.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	mismatchDER, err := x509.CreateCertificate(rand.Reader, template, template, &mismatchKey.PublicKey, mismatchKey)
	if err != nil {
		t.Fatal("synthetic mismatch certificate unavailable")
	}
	mismatch := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GM-Playwright-Run", nonce)
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	mismatch.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{mismatchDER}, PrivateKey: mismatchKey}}, MinVersion: tls.VersionTLS12}
	mismatch.Config.ErrorLog = log.New(io.Discard, "", 0)
	mismatch.StartTLS()
	defer mismatch.Close()
	handler := app.Handler()
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-GM-Playwright-Run", nonce)
		handler.ServeHTTP(w, r)
	})
	server.StartTLS()
	_, calendar, _ := fixture()
	if app.SeedCalendar(ctx, calendar) != nil {
		t.Fatal("calendar fixture unavailable")
	}
	// Retained history exceeds the legacy project limit and includes root-owned
	// and NULL-employee facts; it deliberately predates the planning year.
	if _, err = store.DB.Exec(ctx, "INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES('pw-history','PW retained history','2026-01-05','working',$1)", calendar.ID); err != nil {
		t.Fatal("history project unavailable")
	}
	if _, err = store.DB.Exec(ctx, ` INSERT INTO tasks(project_id,id,title,status,legacy_actual_minutes) VALUES('pw-history','history-root','History root','todo',23);
 INSERT INTO tasks(project_id,id,parent_id,title,status,legacy_actual_minutes) VALUES('pw-history','history-child','history-root','History child','todo',17);
 INSERT INTO employees(id,name,daily_minutes) VALUES('history-employee','Synthetic historical employee',480);
 INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,comment) SELECT 'pw-entry-'||lpad(n::text,4,'0'),'pw-history',CASE WHEN n=1 THEN 'history-root' ELSE 'history-child' END,CASE WHEN n=1 THEN NULL ELSE 'history-employee' END,'2024-02-29',1,'Synthetic retained fact' FROM generate_series(1,1002) AS n;`); err != nil {
		t.Fatal("history fixture unavailable")
	}

	key, _ := x509.MarshalPKCS8PrivateKey(support)
	input := map[string]any{"format": "gm-vector-playwright-fixture", "schema_version": 1, "engine": engine, "nonce": nonce, "origin": cfg.Origin, "certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})), "mismatch_origin": mismatch.URL, "mismatch_certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: mismatchDER})), "username": "pw-admin", "password": token() + "-synthetic", "report_private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})), "calendar_id": calendar.ID, "history_project": "pw-history"}
	raw, _ := json.Marshal(input)
	file := filepath.Join(runDir, "fixture.json")
	ipc, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("private IPC unavailable")
	}
	if _, err = ipc.Write(raw); err != nil {
		ipc.Close()
		t.Fatal("private IPC write failed")
	}
	if ipc.Close() != nil {
		t.Fatal("private IPC close failed")
	}
	// Child output is private; Playwright call logs can contain input values.
	private, err := os.OpenFile(filepath.Join(runDir, "process.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("private child log unavailable")
	}
	defer private.Close()
	environment := []string{}
	for _, name := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "XDG_CACHE_HOME", "PLAYWRIGHT_BROWSERS_PATH", "LD_LIBRARY_PATH", "GIO_EXTRA_MODULES", "GST_PLUGIN_PATH", "GST_PLUGIN_SYSTEM_PATH", "FONTCONFIG_PATH", "XDG_DATA_DIRS"} {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	environment = append(environment, "GMA_PW_FIXTURE_FILE="+file)
	if os.Getenv("GMA_PW_DEBUG_DIRECTORY") != "" {
		environment = append(environment, "GMA_PW_PRIVATE_DEBUG=1")
	}
	log := &pwBoundedLog{writer: private, remaining: 1048576}
	cmd := pwCommand(ctx, node, web, engine, environment, log)
	runErr := cmd.Run()
	retained := false
	retain := func() {
		if retained {
			return
		}
		retained = true
		if retainPWFailure(os.Getenv("GMA_PW_DEBUG_DIRECTORY"), runDir, engine) != nil {
			t.Error("explicit private browser diagnostics could not be retained")
		}
	}
	if log.truncated {
		retain()
		t.Fatal("private browser diagnostics exceeded 1MiB bound")
	}
	receiptFile := filepath.Join(runDir, "receipt.json")
	info, statErr := os.Lstat(receiptFile)
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 262144 {
		retain()
		t.Fatal("required private engine receipt unavailable")
	}
	receipt, readErr := os.ReadFile(receiptFile)
	if readErr != nil || validatePWReceipt(receipt, engine, nonce) != nil || runErr != nil {
		retain()
		var partial pwReceipt
		_ = json.Unmarshal(receipt, &partial)
		phase := "startup"
		for i, id := range pwScenarios {
			if i >= len(partial.Scenarios) || partial.Scenarios[i].ID != id {
				phase = id
				break
			}
		}
		site := "unknown"
		if regexp.MustCompile(`^(critical\.spec|fixtures|contracts)\.ts:[0-9]+:[0-9]+$`).MatchString(partial.FailureSite) {
			site = partial.FailureSite
		}
		t.Fatalf("three-engine scenario failed; engine=%s phase=%s site=%s class=Error; private output not published", engine, phase, site)
	}
	var accepted pwReceipt
	if json.Unmarshal(receipt, &accepted) != nil {
		t.Fatal("validated receipt unavailable")
	}
	lifecycle := runPWLifecycle(ctx, node, web, engine, runDir, nonce, raw, environment)
	if !passedPWLifecycle(lifecycle) {
		t.Fatalf("browser lifecycle failed; engine=%s category=%s observed=%d remaining=%d forced_cleanup=%t", engine, lifecycle.Reason, lifecycle.Observed, lifecycle.Remaining, lifecycle.Forced)
	}
	t.Logf("Playwright lifecycle engine=%s readiness=owned-page-created signal=SIGINT observed_scope=owned-descendants observed_count=%d remaining=0 forced_cleanup=false", engine, lifecycle.Observed)
	t.Log("Playwright engine=" + engine + " playwright=1.64.1 browser_version=" + accepted.Browser + " browser_revision=" + accepted.Revision + " scenarios=B01-B09 passed=9 skipped=0 retries=0 functional_tls=synthetic-only native_clipboard=" + accepted.NativeClipboard + " clipboard_ui=controlled-success-and-denial")
}
