package dockerbrew

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// homebrewKeyPEM is Homebrew's API signing key "homebrew-1".
//
// Source: https://raw.githubusercontent.com/Homebrew/brew/c6069d2a943b71b0a90d91fc1ac93cb9c1634dcc/Library/Homebrew/api/homebrew-1.pem
// (Homebrew/brew commit c6069d2a943b71b0a90d91fc1ac93cb9c1634dcc),
// sha256 ef2d2c9e0219d485df9f07fff7b037feadc36c93085be9ffefb1390f31a3de1d.
//
// The key file is redistributed under Homebrew's license, reproduced verbatim:
//
//	BSD 2-Clause License
//
//	Copyright (c) 2009-present, Homebrew contributors
//	All rights reserved.
//
//	Redistribution and use in source and binary forms, with or without
//	modification, are permitted provided that the following conditions are met:
//
//	* Redistributions of source code must retain the above copyright notice, this
//	  list of conditions and the following disclaimer.
//
//	* Redistributions in binary form must reproduce the above copyright notice,
//	  this list of conditions and the following disclaimer in the documentation
//	  and/or other materials provided with the distribution.
//
//	THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
//	AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
//	IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
//	DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
//	FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
//	DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
//	SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
//	CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
//	OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
//	OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
//go:embed homebrew-1.pem
var homebrewKeyPEM []byte

// homebrewKeyID is the only key id the index may be signed with.
const homebrewKeyID = "homebrew-1"

// indexKey returns the key the index is verified against. Tests replace it.
var indexKey = func() (*rsa.PublicKey, error) { return parseRSAPublicKey(homebrewKeyPEM) }

func parseRSAPublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("dockerbrew: signing key is not a PEM public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("dockerbrew: parse signing key: %w", err)
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("dockerbrew: signing key is not RSA")
	}
	return pub, nil
}

// ErrBadSignature reports an index whose signature does not verify.
var ErrBadSignature = errors.New("dockerbrew: formula index signature is invalid")

// VerifyIndex verifies Homebrew's signed formula index against the embedded
// homebrew-1 key and returns the signed payload.
func VerifyIndex(jws []byte) ([]byte, error) {
	pub, err := indexKey()
	if err != nil {
		return nil, err
	}
	return verifyIndexWith(jws, pub)
}

type jwsDocument struct {
	Payload    *string `json:"payload"`
	Signatures []struct {
		Header struct {
			Kid string `json:"kid"`
		} `json:"header"`
		Protected string `json:"protected"`
		Signature string `json:"signature"`
	} `json:"signatures"`
}

type jwsProtected struct {
	Alg  string   `json:"alg"`
	B64  *bool    `json:"b64"`
	Crit []string `json:"crit"`
}

// verifyIndexWith checks a JWS JSON-serialization document with an unencoded
// payload (RFC 7797): the protected header must say PS512 with b64=false and
// list b64 as critical, the signing input is the protected header exactly as
// transmitted, ".", and the raw payload string.
func verifyIndexWith(jws []byte, pub *rsa.PublicKey) ([]byte, error) {
	var doc jwsDocument
	if err := json.Unmarshal(jws, &doc); err != nil {
		return nil, fmt.Errorf("dockerbrew: parse formula index: %w", err)
	}
	if doc.Payload == nil {
		return nil, errors.New("dockerbrew: formula index has no payload")
	}
	if len(doc.Signatures) != 1 {
		return nil, fmt.Errorf("dockerbrew: formula index has %d signatures, want 1", len(doc.Signatures))
	}
	sig := doc.Signatures[0]
	if sig.Header.Kid != homebrewKeyID {
		return nil, fmt.Errorf("dockerbrew: formula index signed with key %q, want %q", sig.Header.Kid, homebrewKeyID)
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(sig.Protected)
	if err != nil {
		return nil, fmt.Errorf("dockerbrew: decode protected header: %w", err)
	}
	var hdr jwsProtected
	if err := json.Unmarshal(rawHeader, &hdr); err != nil {
		return nil, fmt.Errorf("dockerbrew: parse protected header: %w", err)
	}
	if hdr.Alg != "PS512" {
		return nil, fmt.Errorf("dockerbrew: formula index algorithm %q, want PS512", hdr.Alg)
	}
	if hdr.B64 == nil || *hdr.B64 || len(hdr.Crit) != 1 || hdr.Crit[0] != "b64" {
		return nil, errors.New("dockerbrew: formula index protected header must be b64=false with crit [b64]")
	}
	// Homebrew pads the signature; RFC 7515 base64url does not. Accept both.
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(sig.Signature, "="))
	if err != nil {
		return nil, fmt.Errorf("dockerbrew: decode signature: %w", err)
	}
	h := sha512.New()
	h.Write([]byte(sig.Protected))
	h.Write([]byte{'.'})
	h.Write([]byte(*doc.Payload))
	if err := rsa.VerifyPSS(pub, crypto.SHA512, h.Sum(nil), signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
		return nil, ErrBadSignature
	}
	return []byte(*doc.Payload), nil
}
