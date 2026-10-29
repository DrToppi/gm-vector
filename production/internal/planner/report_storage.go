package planner

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const reportMaxBytes = 256 << 10
const reportMaxCount = 256
const reportMaxTotalBytes = 64 << 20
const reportPerMinute = 10

// Only the envelope is checked here. The support private key is deliberately
// absent: accepting a report does not prove that ciphertext decrypts or that its
// plaintext is harmless. Support must treat decrypted submissions as untrusted.
func validateReportEnvelope(encrypted string, key ReportPublicKey) bool {
	if len(encrypted) > reportMaxBytes || validateReportPublicKey(key) != nil {
		return false
	}
	parts := strings.Split(encrypted, ".")
	if len(parts) != 5 || len(parts[0]) > 1024 {
		return false
	}
	decoded := make([][]byte, 5)
	for i, part := range parts {
		b, err := base64.RawURLEncoding.Strict().DecodeString(part)
		if err != nil || len(b) == 0 || base64.RawURLEncoding.EncodeToString(b) != part {
			return false
		}
		decoded[i] = b
	}
	// Require exactly the supported header fields, rejecting duplicate JSON keys
	// as well as extensions, remote key references and attacker-controlled labels.
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(decoded[0]))
	tok, err := decoder.Token()
	if err != nil || tok != json.Delim('{') {
		return false
	}
	fields = make(map[string]json.RawMessage)
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			return false
		}
		label, ok := name.(string)
		if !ok {
			return false
		}
		if _, exists := fields[label]; exists {
			return false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
		fields[label] = value
	}
	if _, err = decoder.Token(); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	expected := map[string]string{"alg": "RSA-OAEP-256", "enc": "A256GCM", "kid": key.Kid, "typ": "gma-bug-report+jwe", "cty": "application/json"}
	if len(fields) != len(expected) {
		return false
	}
	for name, expectedValue := range expected {
		var value string
		if json.Unmarshal(fields[name], &value) != nil || value != expectedValue {
			return false
		}
	}
	return len(decoded[1]) == 384 && len(decoded[2]) == 12 && len(decoded[4]) == 16
}

func reportStorageUnavailable() error {
	return appError{503, "report_storage_unavailable", "Хранилище отчётов недоступно. Скачайте зашифрованный файл и передайте поддержке."}
}

// writeReport serializes quota checking and durable publication across processes.
// Filenames are derived only from ciphertext hashes, never request paths or text.
func writeReport(directory, encrypted string, now time.Time) (string, bool, error) {
	if directory == "" || !filepath.IsAbs(directory) {
		return "", false, reportStorageUnavailable()
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", false, reportStorageUnavailable()
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", false, reportStorageUnavailable()
	}
	defer root.Close()
	lock, err := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", false, reportStorageUnavailable()
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return "", false, appError{429, "report_storage_busy", "Хранилище занято. Повторите отправку отчёта."}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	digest := sha256.Sum256([]byte(encrypted))
	id := hex.EncodeToString(digest[:])
	name := id + ".gma-bug"
	if info, err = root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(len(encrypted)) {
			return "", false, reportStorageUnavailable()
		}
		existing, err := root.ReadFile(name)
		if err != nil || !bytes.Equal(existing, []byte(encrypted)) {
			return "", false, reportStorageUnavailable()
		}
		return id, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, reportStorageUnavailable()
	}
	dir, err := root.Open(".")
	if err != nil {
		return "", false, reportStorageUnavailable()
	}
	defer dir.Close()
	entries, err := dir.Readdirnames(reportMaxCount + 2)
	if err != nil && err != io.EOF {
		return "", false, reportStorageUnavailable()
	}
	count, recent := 0, 0
	var total int64
	for _, entry := range entries {
		if entry == ".lock" {
			continue
		}
		info, err := root.Lstat(entry)
		if err != nil || !info.Mode().IsRegular() {
			return "", false, reportStorageUnavailable()
		}
		count++
		total += info.Size()
		if info.ModTime().After(now.Add(-time.Minute)) {
			recent++
		}
	}
	if count >= reportMaxCount || total+int64(len(encrypted)) > reportMaxTotalBytes {
		return "", false, appError{507, "report_storage_full", "Хранилище отчётов заполнено. Скачайте файл и обратитесь к администратору."}
	}
	if recent >= reportPerMinute {
		return "", false, appError{429, "report_rate_limit", "Слишком много отчётов. Повторите через минуту или скачайте файл."}
	}
	pending := ".pending-" + newID()
	file, err := root.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", false, reportStorageUnavailable()
	}
	defer root.Remove(pending)
	_, writeErr := io.WriteString(file, encrypted)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return "", false, reportStorageUnavailable()
	}
	if err = root.Rename(pending, name); err != nil {
		return "", false, reportStorageUnavailable()
	}
	if err = dir.Sync(); err != nil {
		return "", false, reportStorageUnavailable()
	}
	return id, true, nil
}

func (a *App) saveBugReport(w http.ResponseWriter, r *http.Request, _ Principal) error {
	if a.Config.ReportPublicKey == nil || a.Config.ReportDirectory == "" {
		return reportStorageUnavailable()
	}
	r.Body = http.MaxBytesReader(w, r.Body, reportMaxBytes+1024)
	var input struct {
		Encrypted string `json:"encrypted"`
	}
	if err := decode(r, &input); err != nil {
		if ae, ok := err.(appError); ok && ae.Status == 413 {
			return appError{413, "report_too_large", "Отчёт превышает 256 KiB. Скачайте его файлом."}
		}
		return err
	}
	if len(input.Encrypted) > reportMaxBytes {
		return appError{413, "report_too_large", "Отчёт превышает 256 KiB. Скачайте его файлом."}
	}
	if !validateReportEnvelope(input.Encrypted, *a.Config.ReportPublicKey) {
		return bad("Неверный формат зашифрованного отчёта или ключ поддержки")
	}
	id, created, err := writeReport(a.Config.ReportDirectory, input.Encrypted, time.Now())
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	jsonResponse(w, status, map[string]any{"id": id, "stored": true})
	return nil
}
