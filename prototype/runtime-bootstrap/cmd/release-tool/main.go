package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const signingKeyEnvironment = "OCEAN_WATCH_RELEASE_SIGNING_KEY"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("release-tool requires public-key, sign, or verify")
	}
	switch arguments[0] {
	case "public-key":
		privateKey, err := signingKey()
		if err != nil {
			return err
		}
		fmt.Println(hex.EncodeToString(privateKey.Public().(ed25519.PublicKey)))
		return nil
	case "sign":
		flags := flag.NewFlagSet("sign", flag.ContinueOnError)
		input := flags.String("input", "", "input file")
		output := flags.String("output", "", "detached signature file")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if *input == "" || *output == "" {
			return errors.New("sign requires --input and --output")
		}
		privateKey, err := signingKey()
		if err != nil {
			return err
		}
		payload, err := os.ReadFile(*input)
		if err != nil {
			return err
		}
		signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)) + "\n"
		return os.WriteFile(*output, []byte(signature), 0o600)
	case "verify":
		flags := flag.NewFlagSet("verify", flag.ContinueOnError)
		input := flags.String("input", "", "input file")
		signaturePath := flags.String("signature", "", "detached signature file")
		publicKeyHex := flags.String("public-key-hex", "", "trusted public key")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		payload, err := os.ReadFile(*input)
		if err != nil {
			return err
		}
		signatureText, err := os.ReadFile(*signaturePath)
		if err != nil {
			return err
		}
		publicKey, err := hex.DecodeString(*publicKeyHex)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return errors.New("invalid public key")
		}
		signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureText)))
		if err != nil || len(signature) != ed25519.SignatureSize {
			return errors.New("invalid detached signature")
		}
		if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
			return errors.New("signature verification failed")
		}
		fmt.Println("signature verified")
		return nil
	default:
		return fmt.Errorf("unknown release-tool command: %s", arguments[0])
	}
}

func signingKey() (ed25519.PrivateKey, error) {
	raw := strings.TrimSpace(os.Getenv(signingKeyEnvironment))
	if raw == "" {
		return nil, errors.New(signingKeyEnvironment + " is required")
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, errors.New("release signing key must be hex or base64")
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(append([]byte(nil), decoded...)), nil
	default:
		return nil, errors.New("release signing key has an invalid length")
	}
}
