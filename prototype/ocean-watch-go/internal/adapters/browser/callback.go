package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

const CallbackPath = "/oauth/callback"

type Session interface {
	Exchange(context.Context, string, string) (domain.OAuthToken, error)
}

type CallbackServer struct {
	Address       string
	Session       Session
	HeaderTimeout time.Duration
}

func (server CallbackServer) Listen() (net.Listener, error) {
	address := strings.TrimSpace(server.Address)
	if address == "" {
		address = "127.0.0.1:8787"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth callback address: %w", err)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("OAuth callback server must bind a loopback IP address")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind OAuth callback server: %w", err)
	}
	return listener, nil
}

func (server CallbackServer) Serve(ctx context.Context) error {
	if ctx == nil || server.Session == nil {
		return errors.New("OAuth callback server is incomplete")
	}
	listener, err := server.Listen()
	if err != nil {
		return err
	}
	defer listener.Close()
	headerTimeout := server.HeaderTimeout
	if headerTimeout <= 0 {
		headerTimeout = 5 * time.Second
	}
	httpServer := &http.Server{
		Handler:           Handler(server.Session),
		ReadHeaderTimeout: headerTimeout,
	}
	shutdownDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
		case <-shutdownDone:
		}
	}()
	err = httpServer.Serve(listener)
	close(shutdownDone)
	if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func Handler(session Session) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if request.Method != http.MethodGet || request.URL.Path != CallbackPath {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Query().Get("error") != "" {
			http.Error(writer, "OAuth authorization was not completed", http.StatusBadRequest)
			return
		}
		_, err := session.Exchange(
			request.Context(), request.URL.Query().Get("state"), request.URL.Query().Get("auth_code"),
		)
		if err != nil {
			http.Error(writer, "OAuth callback was rejected", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("OAuth authorization completed. You may close this page.\n"))
	})
}
