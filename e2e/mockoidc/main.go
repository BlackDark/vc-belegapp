// Command mockoidc is the OIDC provider Playwright logs in against.
// It never leaves the test machine and queues the same allowed user
// for every authorize redirect.
package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/oauth2-proxy/mockoidc"
)

func main() {
	addr := os.Getenv("FAKE_OIDC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18082"
	}
	subject := os.Getenv("FAKE_OIDC_SUBJECT")
	if subject == "" {
		subject = "e2e-user"
	}
	server, err := mockoidc.NewServer(nil)
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Start(ln, nil); err != nil {
		log.Fatal(err)
	}
	for range 32 {
		server.QueueUser(&mockoidc.MockUser{
			Subject:       subject,
			Email:         "eduard@example.de",
			EmailVerified: true,
		})
	}
	payload, err := json.Marshal(map[string]string{
		"issuer":        server.Issuer(),
		"client_id":     server.ClientID,
		"client_secret": server.ClientSecret,
	})
	if err != nil {
		log.Fatal(err)
	}
	if path := os.Getenv("FAKE_OIDC_CONFIG"); path != "" {
		if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("mockoidc %s", server.Issuer())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = server.Shutdown()
}
