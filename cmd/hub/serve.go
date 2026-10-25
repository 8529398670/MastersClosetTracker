package main

import (
	log "log"
	os "os"
	signal "os/signal"
	syscall "syscall"
	time "time"

	fiber "github.com/gofiber/fiber/v3"

	config "github.com/0187773933/MastersClosetTracker/server/config"
	db "github.com/0187773933/MastersClosetTracker/server/db"
	hub "github.com/0187773933/MastersClosetTracker/server/hub"
	middleware "github.com/0187773933/MastersClosetTracker/server/middleware"
)

func openHub( cfg *config.Config ) ( store *db.Store , hub_store *hub.Store , err error ) {
	store , err = db.Open( cfg , hub.Buckets... )
	if err != nil { return }
	hub_store , err = hub.Open( store , hub.Limits{
		MaxDocBytes: cfg.HubMaxDocBytes,
		MaxFuture:   cfg.HubMaxFuture,
		PullPageMax: cfg.HubPullPageMax,
	} )
	if err != nil {
		store.Close()
		store = nil
	}
	return
}

func run() ( err error ) {
	cfg , err := config.Load()
	if err != nil { return }

	store , hub_store , err := openHub( cfg )
	if err != nil { return }
	defer store.Close()

	control , err := hub.ListenControl( hub_store , cfg )
	if err != nil { return }
	defer control.Close()

	app := fiber.New( fiber.Config{
		AppName:      cfg.ProjectName,
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
	app.Use( middleware.RateLimit( cfg , "/sync/" ) )
	handlers := &hub.Handlers{
		Hub:          hub_store,
		MaxPushBytes: cfg.HubMaxPushBytes,
		RateMax:      cfg.HubKeyRateMax,
		RateWindow:   cfg.HubKeyRateWindow,
		Now:          time.Now,
	}
	handlers.Register( app )

	shutdown := make( chan os.Signal , 1 )
	signal.Notify( shutdown , os.Interrupt , syscall.SIGTERM )
	go func() {
		<-shutdown
		log.Println( "[shutdown] draining connections..." )
		control.Close()
		app.ShutdownWithTimeout( 10 * time.Second )
	}()

	status , _ := hub_store.Status()
	log.Printf( "[start] %s %s listening on %s" , cfg.ProjectName , version , cfg.ListenAddress() )
	log.Printf( "[config] %s" , cfg.ConfigFile )
	log.Printf( "[state]  %s" , cfg.AppDir )
	log.Printf( "[control] %s" , control.Path() )
	log.Printf( "[data]   epoch %s, %d changes, %v" , status.Epoch , status.MaxSeq , status.Counts )
	keys , _ := hub_store.ListKeys()
	if len( keys ) == 0 {
		log.Printf( "[keys]   no station keys yet -- make one per station with: mct-hub keys create -name \"Front desk\"" )
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
