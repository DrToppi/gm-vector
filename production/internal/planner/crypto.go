package planner

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strings"
	"time"
	"unicode"
)

func random(n int) []byte {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return b
}
func token() string        { return base64.RawURLEncoding.EncodeToString(random(32)) }
func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func passwordHash(s string) string {
	salt := random(16)
	key := argon2.IDKey([]byte(s), salt, 3, 64*1024, 2, 32)
	return "argon2id$3$65536$2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
}
func passwordOK(s, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "argon2id" || parts[1] != "3" || parts[2] != "65536" || parts[3] != "2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(s), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}
func validPassword(s string) bool { return len([]rune(s)) >= 15 && len([]rune(s)) <= 128 }
func seal(key, plain []byte) ([]byte, error) {
	c, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(c)
	if e != nil {
		return nil, e
	}
	nonce := random(g.NonceSize())
	return g.Seal(nonce, nonce, plain, []byte("GMA|Planner:v1")), nil
}
func unseal(key, enc []byte) ([]byte, error) {
	c, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(c)
	if e != nil || len(enc) < g.NonceSize() {
		return nil, errors.New("invalid encrypted secret")
	}
	return g.Open(nil, enc[:g.NonceSize()], enc[g.NonceSize():], []byte("GMA|Planner:v1"))
}
func totpSecret() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random(20))
}
func totpAt(secret string, step int64) string {
	key, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if e != nil {
		return ""
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(buf)
	b := m.Sum(nil)
	o := b[len(b)-1] & 15
	n := binary.BigEndian.Uint32(b[o:o+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func totpStep(secret, code string, last int64, now time.Time) int64 {
	code = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, code)
	if len(code) != 6 {
		return -1
	}
	for d := int64(-1); d <= 1; d++ {
		step := now.Unix()/30 + d
		if step > last && subtle.ConstantTimeCompare([]byte(totpAt(secret, step)), []byte(code)) == 1 {
			return step
		}
	}
	return -1
}
