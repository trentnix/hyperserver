// Command blog serves the Markdown blog with HyperServer's email/password login.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 8085, "loopback HTTP port")
	dataDir := flag.String("data-dir", "blog-data", "directory for blog, account, and session storage")
	origin := flag.String("public-origin", "", "public HTTP(S) origin for RSS links (defaults to the loopback address)")
	title := flag.String("title", "Trent Nix", "blog title")
	provision := flag.Bool("create-admin", false, "create the admin account from BLOG_ADMIN_EMAIL and BLOG_ADMIN_PASSWORD, then exit")
	flag.Parse()
	if *port < 1 || *port > 65535 {
		log.Fatal("port must be between 1 and 65535")
	}
	if *origin == "" {
		*origin = fmt.Sprintf("http://127.0.0.1:%d", *port)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	app, err := newApplication(startup, settings{
		dataDir: *dataDir, publicOrigin: *origin, title: *title,
		description: "Trent is a technologist interested in building and delivering better software.",
		adminEmail:  os.Getenv("BLOG_ADMIN_EMAIL"),
		authKey:     os.Getenv("BLOG_AUTH_KEY"), sessionKey: os.Getenv("BLOG_SESSION_KEY"),
	})
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	if *provision {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err := createAdmin(ctx, app.accounts, os.Getenv("BLOG_ADMIN_EMAIL"), os.Getenv("BLOG_ADMIN_PASSWORD"))
		if err := errors.Join(err, app.Shutdown()); err != nil {
			log.Fatal(err)
		}
		log.Print("Admin account created")
		return
	}
	if err := app.Run(ctx, func(context.Context) (*http.Server, net.Listener, error) {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
		if err != nil {
			return nil, nil, err
		}
		log.Printf("Blog listening at http://%s", listener.Addr())
		return newHTTPServer(app), listener, nil
	}); err != nil {
		log.Fatal(err)
	}
}
