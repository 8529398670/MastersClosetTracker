// Command mct is the Masters Closet Tracker station: the web app the closet
// runs on each laptop and on the public node. Its sibling, the sync hub, is
// cmd/hub.
//
// This file only wires things together -- load config, open the database,
// build the app, register routes, start and stop cleanly. Every decision of
// substance lives in a package under server/. If you find yourself adding
// business logic here, it belongs in server/routes/ or a new package instead.
//
// The same binary is also the admin CLI:
//
//	mct                     run the server
//	mct manage list-users   account and data administration
//	mct config check        create, check, or read config.yaml
//	mct health              the container's healthcheck
//	mct version             build information
//
// --config <path> anywhere on the command line picks the config file for
// every one of those.
package main

import (
	embed "embed"
	fmt "fmt"
	fs "io/fs"
	log "log"
	os "os"
	signal "os/signal"
	runtime "runtime"
	syscall "syscall"
	time "time"
	// The IANA time zone database, compiled in: Windows and the Alpine image
	// ship none, and without it the org time zone silently becomes UTC.
	_ "time/tzdata"

	fiber "github.com/gofiber/fiber/v3"

	assets "github.com/0187773933/MastersClosetTracker/server/assets"
	bootstrap "github.com/0187773933/MastersClosetTracker/server/bootstrap"
	closet "github.com/0187773933/MastersClosetTracker/server/closet"
	config "github.com/0187773933/MastersClosetTracker/server/config"
	control "github.com/0187773933/MastersClosetTracker/server/control"
	db "github.com/0187773933/MastersClosetTracker/server/db"
	docs "github.com/0187773933/MastersClosetTracker/server/docs"
	healthcheck "github.com/0187773933/MastersClosetTracker/server/healthcheck"
	index "github.com/0187773933/MastersClosetTracker/server/index"
	language "github.com/0187773933/MastersClosetTracker/server/language"
	manage "github.com/0187773933/MastersClosetTracker/server/manage"
	middleware "github.com/0187773933/MastersClosetTracker/server/middleware"
	replica "github.com/0187773933/MastersClosetTracker/server/replica"
	models "github.com/0187773933/MastersClosetTracker/server/models"
	routes "github.com/0187773933/MastersClosetTracker/server/routes"
	security "github.com/0187773933/MastersClosetTracker/server/security"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
	static "github.com/0187773933/MastersClosetTracker/server/static"
)

// Stamped by build.sh via -ldflags. The defaults are what you get from a
// plain `go build`, which is a useful signal in itself.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// The frontend, compiled into the binary.
//
// This is what makes build.sh able to produce one file that runs anywhere:
// no ./static directory to ship alongside it, no language.yaml to forget.
// The embed directives have to live in the package at the module root,
// because go:embed cannot reference paths outside its own package directory
// -- which is the only reason this is here rather than inside server/assets.
//
// "all:" includes files whose names begin with "." or "_", which a normal
// embed pattern skips.
//
//go:embed all:static
var embeddedStaticRoot embed.FS

//go:embed configs/language.yaml
var embeddedLanguage []byte

// embeddedStatic re-roots the embedded tree at ./static, so a request for
// "css/style.css" does not have to know it lives at "static/css/style.css"
// inside the binary. This makes it interchangeable with the os.Root
// filesystem server/assets opens for ./static on disk.
func embeddedStatic() ( fsys fs.FS , err error ) {
	fsys , err = fs.Sub( embeddedStaticRoot , "static" )
	return
}

func main() {
	config.Use( config.Station )
	arguments , err := config.StripConfigFlag( os.Args[ 1: ] )
	if err != nil {
		fmt.Fprintf( os.Stderr , "error: %v\n" , err )
		os.Exit( 2 )
	}

	// Subcommands before anything else: `manage` must work on a database the
	// server cannot currently open, which is exactly when you need it.
	if len( arguments ) > 0 {
		switch arguments[ 0 ] {
		case "manage":
			if err := manage.Run( arguments[ 1: ] ); err != nil {
				fmt.Fprintf( os.Stderr , "error: %v\n" , err )
				os.Exit( 1 )
			}
			return
		case "config":
			if err := config.RunCommand( arguments[ 1: ] ); err != nil {
				fmt.Fprintf( os.Stderr , "error: %v\n" , err )
				os.Exit( 1 )
			}
			return
		case "health":
			if err := healthcheck.Run(); err != nil {
				fmt.Fprintf( os.Stderr , "unhealthy: %v\n" , err )
				os.Exit( 1 )
			}
			return
		case "version" , "-version" , "--version":
			fmt.Printf( "Masters Closet Tracker %s (commit %s, built %s, %s/%s, %s)\n" ,
				version , commit , buildDate , runtime.GOOS , runtime.GOARCH , runtime.Version() )
			return
		case "-h" , "--help" , "help":
			fmt.Printf( "Masters Closet Tracker %s\n\nUsage:\n  mct [--config path]                 run the web server\n  mct manage <cmd>                    account and data administration\n  mct config <cmd>                    create, check, or read config.yaml\n  mct health                          exit 0 if the local server answers\n  mct version                         build information\n\n" , version )
			manage.Usage()
			fmt.Println( "" )
			config.CommandUsage()
			return
		}
	}

	if err := run(); err != nil {
		log.Fatalf( "[fatal] %v" , err )
	}
}

func run() ( err error ) {
	cfg , err := config.Load()
	if err != nil { return }

	embedded , err := embeddedStatic()
	if err != nil { return }

	// Disk when it exists, the embedded copy otherwise. See server/assets.
	bundle , err := assets.Resolve( cfg.StaticDir , cfg.LanguageFile , embedded , embeddedLanguage )
	if err != nil { return }

	store , err := db.Open( cfg , closet.Buckets... )
	if err != nil { return }
	defer store.Close()

	// The closet's data: synced records, the settings over them, and the
	// in-memory index the pages read through.
	docs_store , err := docs.Open( store , nil )
	if err != nil { return }
	if behind := docs_store.Clock().Behind(); behind > 5*time.Second {
		log.Printf( "[clock] warning: this machine's clock is %v behind the last change it recorded -- check the date and time" , behind.Round( time.Second ) )
	}
	settings_service , err := settings.New( docs_store , store , cfg )
	if err != nil { return }
	// Worked out once per start: visits and sync status carry it, and a
	// rename on the Settings page takes effect on the next start.
	station_name := settings_service.StationName()
	closet_index := index.New()
	if err = closet_index.Attach( docs_store ); err != nil { return }
	closet_service := closet.New( docs_store , closet_index , settings_service , station_name )

	lang , err := language.Parse( bundle.LanguageYAML )
	if err != nil { return }

	static_server , err := static.New( bundle.Static , "account.html" )
	if err != nil { return }

	if err = bootstrap.EnsureFirstAdmin( store , cfg , "Admin" ); err != nil { return }

	// The control socket is what lets `manage` run against a live server.
	// bolt hands the database to exactly one process, so without it every
	// admin command would need the server stopped first. It is started after
	// the database opens, which means the socket existing is itself proof
	// that a live process holds the lock.
	control_server , err := control.Listen( store , cfg )
	if err != nil { return }
	defer control_server.Close()
	control_server.SetData( docs_store )

	guard := security.New( store , cfg )
	handlers := routes.New( store , cfg , guard , lang , static_server )
	handlers.Domain = &routes.Domain{
		Closet:   closet_service,
		Settings: settings_service,
		Docs:     docs_store,
	}

	// Sync with the hub, when one is configured. Stopped before the
	// database closes (defers run in reverse), so no round is cut off
	// half-applied.
	if cfg.SyncEnabled {
		syncer := replica.New( docs_store , replica.Options{
			HubURL:  cfg.SyncHubURL,
			APIKey:  cfg.SyncAPIKey,
			Station: station_name,
			Poll:    cfg.SyncPoll,
		} )
		syncer.Start()
		defer syncer.Stop()
	}

	app := fiber.New( fiber.Config{
		AppName: cfg.ProjectName,
		// Copy every value read from a request (params, queries, headers)
		// instead of handing out views into fasthttp's reused buffers. Ids
		// from a URL end up as keys in the in-memory index, and a zero-copy
		// string there is silently overwritten by the next request.
		Immutable: true,
		// An empty Server header keeps the response from advertising which
		// framework and version is running -- free reconnaissance otherwise.
		ServerHeader: "",
		BodyLimit:    cfg.BodyLimit,
		// Timeouts are what stop a handful of slow or stalled connections
		// from holding sockets open until the process runs out of them.
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  75 * time.Second,
		// Forwarded headers are believed only from the configured proxies,
		// and only once validated as IP addresses -- see server.trust_proxy
		// in config.yaml. Without the allowlist fiber trusts no proxy at all.
		TrustProxy:         cfg.TrustProxy,
		TrustProxyConfig:   fiber.TrustProxyConfig{ Proxies: cfg.TrustedProxies },
		EnableIPValidation: true,
		ProxyHeader:        proxyHeader( cfg ),
		ErrorHandler: func( c fiber.Ctx , handler_err error ) ( response_err error ) {
			// Never hand an internal error message to a client. The detail
			// goes to the log, where the operator can see it.
			code := fiber.StatusInternalServerError
			var fiber_err *fiber.Error
			if ok := asFiberError( handler_err , &fiber_err ); ok {
				code = fiber_err.Code
			}
			if code >= 500 {
				log.Printf( "[error] %s %s: %v" , c.Method() , c.Path() , handler_err )
			}
			response_err = c.Status( code ).JSON( fiber.Map{ "error": statusText( code ) } )
			return
		},
	} )

	app.Use( middleware.Recover( cfg ) )
	app.Use( middleware.SecurityHeaders( cfg ) )
	app.Use( middleware.RateLimit( cfg ) )

	handlers.Register( app )

	stop_purging := startPurgeLoop( store )
	defer close( stop_purging )

	// Shut down on a signal rather than being killed, so in-flight requests
	// finish and bolt closes cleanly instead of leaving a locked file behind.
	shutdown := make( chan os.Signal , 1 )
	signal.Notify( shutdown , os.Interrupt , syscall.SIGTERM )
	go func() {
		<-shutdown
		log.Println( "[shutdown] draining connections..." )
		// Close the control socket first: it is the door other processes
		// come through, and letting a manage command start against a
		// database that is about to close would fail confusingly.
		control_server.Close()
		app.ShutdownWithTimeout( 10 * time.Second )
	}()

	log.Printf( "[start] %s %s listening on %s (secure cookies: %v)" , cfg.ProjectName , version , cfg.ListenAddress() , cfg.SecureCookies )
	log.Printf( "[open]  %s" , cfg.LocalURL() )
	if cfg.ConfigCreated {
		log.Printf( "[config] %s (created, with a new secret key -- back it up)" , cfg.ConfigFile )
	} else {
		log.Printf( "[config] %s" , cfg.ConfigFile )
	}
	if cfg.ConfigWarning != "" {
		log.Printf( "[config] warning: %s" , cfg.ConfigWarning )
	}
	log.Printf( "[state]  %s" , cfg.AppDir )
	log.Printf( "[assets] %s" , bundle.Source )
	log.Printf( "[control] %s" , control_server.Path() )
	live_clients , counted_visits := closet_index.Counts()
	log.Printf( "[data]   %d clients, %d visits, database %s, station %q" , live_clients , counted_visits , docs_store.Node() , station_name )
	if cfg.SyncEnabled {
		log.Printf( "[sync]   hub %s, %d changes waiting to push" , cfg.SyncHubURL , docs_store.PendingCount() )
	} else {
		log.Printf( "[sync]   off (no sync.hub_url in config)" )
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

func asFiberError( err error , out **fiber.Error ) ( ok bool ) {
	converted , ok := err.( *fiber.Error )
	if ok {
		*out = converted
	}
	return
}

func statusText( code int ) ( result string ) {
	switch {
	case code == fiber.StatusNotFound:
		result = "not found"
	case code == fiber.StatusUnauthorized:
		result = "not authenticated"
	case code == fiber.StatusForbidden:
		result = "forbidden"
	case code == fiber.StatusTooManyRequests:
		result = "too many requests"
	case code >= 500:
		result = "internal error"
	default:
		result = "bad request"
	}
	return
}

// startPurgeLoop drops expired sessions, expired login tokens, and the API
// keys and invite links that have been dead long enough to stop being worth
// listing. None of it is a correctness issue -- every one of those is
// already refused on read -- so this runs quietly in the background purely to
// keep the bolt file from growing forever.
func startPurgeLoop( store *db.Store ) ( stop chan struct{} ) {
	stop = make( chan struct{} )
	go func() {
		ticker := time.NewTicker( time.Hour )
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sessions , _ := models.PurgeExpiredSessions( store )
				tokens , _ := models.PurgeExpiredLoginTokens( store )
				keys , _ := models.PurgeDeadAPIKeys( store )
				invites , _ := models.PurgeDeadInviteLinks( store )
				if sessions > 0 || tokens > 0 || keys > 0 || invites > 0 {
					fmt.Printf( "[purge] removed %d expired sessions, %d expired login tokens, %d dead api keys, %d dead invite links\n" , sessions , tokens , keys , invites )
				}
			}
		}
	}()
	return
}
