package planner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	ber "github.com/go-asn1-ber/asn1-ber"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLDAPVerifiedTLSAndRealSearch(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic directory"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if e = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	listener, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		send := func(id int64, op *ber.Packet) error {
			packet := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
			packet.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
			packet.AppendChild(op)
			_, e := conn.Write(packet.Bytes())
			return e
		}
		result := func(tag ber.Tag) *ber.Packet {
			p := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "")
			p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, 0, ""))
			p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
			p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
			return p
		}
		for {
			packet, e := ber.ReadPacket(conn)
			if e != nil {
				done <- e
				return
			}
			id := packet.Children[0].Value.(int64)
			switch packet.Children[1].Tag {
			case 0:
				if e = send(id, result(1)); e != nil {
					done <- e
					return
				}
			case 3:
				entry := ber.Encode(ber.ClassApplication, ber.TypeConstructed, 4, nil, "")
				entry.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "uid=synthetic,dc=example", ""))
				attrs := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
				for name, value := range map[string]string{"entryUUID": "stable-id", "displayName": "Synthetic User", "mail": "user@example.test", "uid": "synthetic", "enabled": "TRUE"} {
					a := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
					a.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, ""))
					values := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
					values.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, value, ""))
					a.AppendChild(values)
					attrs.AppendChild(a)
				}
				entry.AppendChild(attrs)
				if e = send(id, entry); e != nil {
					done <- e
					return
				}
				done <- send(id, result(5))
				return
			default:
				done <- nil
				return
			}
		}
	}()
	c := fixtureLDAP()
	c.URL = "ldaps://" + listener.Addr().String()
	c.CAFile = ca
	rows, e := c.readEntries(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || rows[0].ID != "stable-id" || !*rows[0].Active {
		t.Fatalf("unexpected directory result: %#v", rows)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
