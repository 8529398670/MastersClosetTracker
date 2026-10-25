package hub

import (
	json "encoding/json"
	log "log"
	strconv "strconv"
	strings "strings"
	time "time"

	fiber "github.com/gofiber/fiber/v3"
	limiter "github.com/gofiber/fiber/v3/middleware/limiter"

	syncproto "github.com/0187773933/MastersClosetTracker/server/syncproto"
)

// Handlers is the hub's HTTP surface: three sync calls and a health check.
type Handlers struct {
	Hub          *Store
	MaxPushBytes int
	RateMax      int
	RateWindow   time.Duration
	Now          func() time.Time
}

const localKey = "hub_key"

func ( handlers *Handlers ) Register( app *fiber.App ) {
	app.Get( "/api/health" , func( c fiber.Ctx ) ( err error ) {
		err = c.JSON( fiber.Map{ "status": "ok" } )
		return
	} )
	sync := app.Group( "/sync/v1" , handlers.authenticate , handlers.rateLimit() )
	sync.Get( "/info" , handlers.Info )
	sync.Post( "/push" , handlers.Push )
	sync.Get( "/pull" , handlers.Pull )
}

func refuse( c fiber.Ctx , status int , message string ) ( err error ) {
	err = c.Status( status ).JSON( syncproto.ErrorResponse{ Error: message } )
	return
}

// authenticate requires a live station key.
func ( handlers *Handlers ) authenticate( c fiber.Ctx ) ( err error ) {
	header := c.Get( fiber.HeaderAuthorization )
	credential , found := strings.CutPrefix( header , "Bearer " )
	if found == false {
		err = refuse( c , fiber.StatusUnauthorized , "a station key is required" )
		return
	}
	key , ok := handlers.Hub.Authenticate( credential )
	if ok == false {
		err = refuse( c , fiber.StatusUnauthorized , "station key is unknown or revoked" )
		return
	}
	c.Locals( localKey , key.ID )
	err = c.Next()
	return
}

// rateLimit is per station key rather than per address: every station in
// the closet reaches the hub from the same public address.
func ( handlers *Handlers ) rateLimit() ( handler fiber.Handler ) {
	if handlers.RateMax == 0 {
		handler = func( c fiber.Ctx ) ( err error ) {
			err = c.Next()
			return
		}
		return
	}
	handler = limiter.New( limiter.Config{
		Max:        handlers.RateMax,
		Expiration: handlers.RateWindow,
		KeyGenerator: func( c fiber.Ctx ) ( result string ) {
			result , _ = c.Locals( localKey ).( string )
			return
		},
		LimitReached: func( c fiber.Ctx ) ( err error ) {
			err = refuse( c , fiber.StatusTooManyRequests , "too many requests from this station key" )
			return
		},
	} )
	return
}

func ( handlers *Handlers ) Info( c fiber.Ctx ) ( err error ) {
	err = c.JSON( handlers.Hub.Info( handlers.Now() ) )
	return
}

func ( handlers *Handlers ) Push( c fiber.Ctx ) ( err error ) {
	body := c.Body()
	if len( body ) > handlers.MaxPushBytes+65536 {
		err = refuse( c , fiber.StatusRequestEntityTooLarge , "push is too large; split it" )
		return
	}
	var request syncproto.PushRequest
	if json.Unmarshal( body , &request ) != nil {
		err = refuse( c , fiber.StatusBadRequest , "malformed push" )
		return
	}
	now := handlers.Now()
	results , push_err := handlers.Hub.Push( request.Docs , now )
	if push_err != nil {
		log.Printf( "[hub] push failed: %v" , push_err )
		err = refuse( c , fiber.StatusInternalServerError , "internal error" )
		return
	}
	err = c.JSON( syncproto.PushResponse{ Results: results , HubEpoch: handlers.Hub.Epoch() , ServerTime: now.UnixMilli() } )
	return
}

func ( handlers *Handlers ) Pull( c fiber.Ctx ) ( err error ) {
	since , parse_err := strconv.ParseUint( c.Query( "since" , "0" ) , 10 , 64 )
	if parse_err != nil {
		err = refuse( c , fiber.StatusBadRequest , "since must be a change number" )
		return
	}
	limit , _ := strconv.Atoi( c.Query( "limit" , "0" ) )
	response , pull_err := handlers.Hub.Pull( since , limit , handlers.Now() )
	if pull_err != nil {
		log.Printf( "[hub] pull failed: %v" , pull_err )
		err = refuse( c , fiber.StatusInternalServerError , "internal error" )
		return
	}
	err = c.JSON( response )
	return
}
