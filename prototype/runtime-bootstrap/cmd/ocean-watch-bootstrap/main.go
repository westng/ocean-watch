package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/westng/ocean-watch/prototype/runtime-bootstrap/bootstrap"
)

var (
	productVersion      = "UNSET"
	pluginVersion       = "UNSET"
	gitCommit           = "UNSET"
	sdkVersion          = bootstrap.DefaultSDKVersion
	trustedPublicKeyHex = "UNSET"
	releaseBaseURL      = "https://github.com/westng/ocean-watch/releases/download"
	allowInsecureTests  = "false"
)

func main() {
	if err := run(); err != nil {
		payload, _ := json.Marshal(map[string]any{
			"ok":    false,
			"error": map[string]string{"code": "runtime_bootstrap_failed", "message": err.Error()},
		})
		fmt.Fprintln(os.Stderr, string(payload))
		os.Exit(2)
	}
}

func run() error {
	route := flag.String("route", "", "domain and action selected by the Plugin launcher")
	cacheRoot := flag.String("cache-root", defaultCacheRoot(), "verified runtime cache root")
	execute := flag.Bool("execute", false, "execute the verified Go runtime")
	flag.Parse()
	if strings.ContainsAny(*route, "\r\n\x00") {
		return errors.New("invalid runtime route")
	}
	publicKeyBytes, err := hex.DecodeString(trustedPublicKeyHex)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return errors.New("bootstrap trust root is not configured")
	}
	goos, goarch, err := bootstrap.CurrentPlatform()
	if err != nil {
		return err
	}
	tagURL := strings.TrimRight(releaseBaseURL, "/") + "/v" + productVersion
	result, err := bootstrap.Ensure(context.Background(), bootstrap.Config{
		Identity: bootstrap.Identity{
			ProductVersion: productVersion,
			PluginVersion:  pluginVersion,
			GitCommit:      gitCommit,
			SDKVersion:     sdkVersion,
		},
		Route:                 *route,
		GOOS:                  goos,
		GOARCH:                goarch,
		CacheRoot:             *cacheRoot,
		ManifestURL:           tagURL + "/runtime-manifest.json",
		SignatureURL:          tagURL + "/runtime-manifest.sig",
		ReleaseBaseURL:        releaseBaseURL,
		TrustedPublicKey:      ed25519.PublicKey(publicKeyBytes),
		AllowInsecureForTests: allowInsecureTests == "true",
	})
	if err != nil {
		return err
	}
	if !*execute || result.Route == "python" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "result": result})
	}
	command := exec.CommandContext(context.Background(), result.AssetPath, flag.Args()...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func defaultCacheRoot() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", "ocean-watch", "runtime")
		}
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, "ocean-watch", "runtime")
}
