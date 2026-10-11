package main

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: verify_sparkle_key <public-key-base64>")
		os.Exit(2)
	}
	encoded, err := io.ReadAll(io.LimitReader(os.Stdin, 256))
	if err != nil {
		fmt.Fprintln(os.Stderr, "read signing key:", err)
		os.Exit(1)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "Sparkle signing key must be a base64-encoded 32-byte seed")
		os.Exit(1)
	}
	actual := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	expected, err := base64.StdEncoding.DecodeString(os.Args[1])
	if err != nil || len(expected) != ed25519.PublicKeySize || subtle.ConstantTimeCompare(actual, expected) != 1 {
		fmt.Fprintln(os.Stderr, "Sparkle signing key does not match the public key embedded in iDeploy")
		os.Exit(1)
	}
}
