package main

import (
	fs "io/fs"
	log "log"
	os "os"
	signal "os/signal"
	filepath "path/filepath"
	syscall "syscall"
	time "time"

	fiber "github.com/gofiber/fiber/v3"

	sitefiles "github.com/0187773933/MastersClosetTracker/static"
	config "github.com/0187773933/MastersClosetTracker/server/config"
	middleware "github.com/0187773933/MastersClosetTracker/server/middleware"
	static "github.com/0187773933/MastersClosetTracker/server/static"
)

// pageCSP is the station's page policy plus what the site's pages need:
// qr-code-styling paints the cross from a data: URL and saves images
// through blob: URLs, and the pass page registers a service worker.
const pageCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"style-src-attr 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"worker-src 'self'; " +
	"manifest-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// homeCSP also lets the landing page embed the closet's public Google
// Calendar.
const homeCSP = pageCSP + "; frame-src https://calendar.google.com"

// allowedFS hides every file that is not the site's (sitefiles.Allowed), so a
// ./static on disk -- which also holds the staff pages -- serves only what
// the embedded copy would.
type allowedFS struct {
	inner fs.FS
}

func ( fsys allowedFS ) Open( name string ) ( file fs.File , err error ) {
	if sitefiles.Allowed( name ) == false {
		err = &fs.PathError{ Op: "open" , Path: name , Err: fs.ErrNotExist }
		return
	}
	file , err = fsys.inner.Open( name )
	return
}

// siteFiles prefers ./static on disk (a source checkout, edit and refresh)
// and falls back to the embedded copy, like the station (server/assets).
func siteFiles( cfg *config.Config ) ( fsys fs.FS , source string , err error ) {
	if cfg.StaticDir != "" {
		if info , stat_err := os.Stat( cfg.StaticDir ); stat_err == nil && info.IsDir() {
			absolute , abs_err := filepath.Abs( cfg.StaticDir )
			if abs_err != nil {
				err = abs_err
				return
			}
			root , root_err := os.OpenRoot( absolute )
			if root_err != nil {
				err = root_err
				return
			}
			fsys , source = allowedFS{ inner: root.FS() } , "disk "+absolute
			return
		}
	}
	fsys , source = allowedFS{ inner: sitefiles.Files } , "embedded"
	return
}

func run() ( err error ) {
	cfg , err := config.Load()
	if err != nil { return }

	files , source , err := siteFiles( cfg )
	if err != nil { return }
	server , err := static.New( files , "site/index.html" )
	if err != nil { return }

	app := fiber.New( fiber.Config{
		AppName:            cfg.ProjectName,
		// Copy request values; see the same setting in the station's main.go.
		Immutable:          true,
		ServerHeader:       "",
		BodyLimit:          cfg.BodyLimit,
		ReadTimeout:        15 * time.Second,
		WriteTimeout:       30 * time.Second,
		IdleTimeout:        75 * time.Second,
		TrustProxy:         cfg.TrustProxy,
		TrustProxyConfig:   fiber.TrustProxyConfig{ Proxies: cfg.TrustedProxies },
		EnableIPValidation: true,
		ProxyHeader:        proxyHeader( cfg ),
	} )
	app.Use( middleware.Recover( cfg ) )
	app.Use( middleware.SecurityHeaders( cfg ) )
	app.Use( middleware.RateLimit( cfg ) )

	page := func( file string , policy string ) ( handler fiber.Handler ) {
		handler = func( c fiber.Ctx ) ( err error ) {
			c.Set( fiber.HeaderContentSecurityPolicy , policy )
			err = server.SendPage( c , file )
			return
		}
		return
	}
	app.Get( "/api/health" , func( c fiber.Ctx ) ( err error ) {
		err = c.JSON( fiber.Map{ "ok": true } )
		return
	} )
	app.Get( "/" , func( c fiber.Ctx ) ( err error ) {
		// The embedded calendar is a cross-origin frame that does not opt in
		// to being embedded under require-corp, so this one page relaxes it.
		c.Set( "Cross-Origin-Embedder-Policy" , "unsafe-none" )
		c.Set( fiber.HeaderContentSecurityPolicy , homeCSP )
		err = server.SendPage( c , "site/index.html" )
		return
	} )
	// Tickets printed before the pass moved to the home page link to
	// /p#<barcode>. A redirect with no "#" of its own keeps the original one
	// (browsers carry it across), so those still land on the pass.
	app.Get( "/p" , func( c fiber.Ctx ) ( err error ) {
		err = c.Redirect().Status( fiber.StatusFound ).To( "/" )
		return
	} )
	app.Get( "/join" , page( "site/join.html" , pageCSP ) )
	// The service worker must be served from the root to control every page.
	app.Get( "/sw.js" , page( "site/sw.js" , pageCSP ) )
	app.Get( "/manifest.webmanifest" , page( "site/manifest.webmanifest" , pageCSP ) )
	app.Get( "/favicon.ico" , func( c fiber.Ctx ) ( err error ) {
		err = server.SendPage( c , "app/img/favicon.ico" )
		return
	} )
	app.Get( "/ical" , func( c fiber.Ctx ) ( err error ) {
		if cfg.SiteICalURL == "" {
			err = c.Status( fiber.StatusNotFound ).SendString( "Not found" )
			return
		}
		err = c.Redirect().Status( fiber.StatusFound ).To( cfg.SiteICalURL )
		return
	} )
	// Everything else: the site's own files only (see allowedFS).
	app.Get( "/*" , server.Handler )

	shutdown := make( chan os.Signal , 1 )
	signal.Notify( shutdown , os.Interrupt , syscall.SIGTERM )
	go func() {
		<-shutdown
		log.Println( "[shutdown] draining connections..." )
		app.ShutdownWithTimeout( 10 * time.Second )
	}()

	log.Printf( "[start] %s %s listening on %s" , cfg.ProjectName , version , cfg.ListenAddress() )
	log.Printf( "[config] %s" , cfg.ConfigFile )
	log.Printf( "[assets] %s" , source )
	if cfg.ConfigWarning != "" {
		log.Printf( "[config] warning: %s" , cfg.ConfigWarning )
	}
	err = app.Listen( cfg.ListenAddress() , fiber.ListenConfig{ DisableStartupMessage: true } )
	return
}

func proxyHeader( cfg *config.Config ) ( result string ) {
	if cfg.TrustProxy {
		result = cfg.ProxyHeader
	}
	return
}
