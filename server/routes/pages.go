package routes

import (
	fiber "github.com/gofiber/fiber/v3"
)

// The closet's pages. Each URL is a route of its own (no single-page-app
// fallback), because the pages read their own path -- /admin/users/<id>
// opens that family's record -- and so a page's URL and where its file
// lives are allowed to differ.

// pageCSP is the template's policy plus inline style attributes, which the
// existing pages use for small layout tweaks (style="gap:14px"). Scripts
// stay same-origin files only -- that is the protection that matters, and
// every inline script was moved out to make it possible. Remove the
// style-src-attr allowance once the pages stop using style attributes.
const pageCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"style-src-attr 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

func ( handlers *Handlers ) sendPage( c fiber.Ctx , file string , policy string ) ( err error ) {
	c.Set( fiber.HeaderContentSecurityPolicy , policy )
	err = handlers.Static.SendPage( c , "app/pages/"+file )
	return
}

func ( handlers *Handlers ) page( file string ) ( handler fiber.Handler ) {
	handler = func( c fiber.Ctx ) ( err error ) {
		err = handlers.sendPage( c , file , pageCSP )
		return
	}
	return
}

func ( handlers *Handlers ) registerPages( app *fiber.App ) {
	app.Get( "/" , handlers.Home )
	app.Get( "/ical" , handlers.ICal )
	app.Get( "/favicon.ico" , func( c fiber.Ctx ) ( err error ) {
		err = handlers.Static.SendPage( c , "app/img/favicon.ico" )
		return
	} )

	// Staff sign-in is the template's account page; the old login form's
	// address goes there too.
	app.Get( "/account" , handlers.Static.ServeIndex )
	app.Get( "/admin/login" , func( c fiber.Ctx ) ( err error ) {
		err = c.Redirect().Status( fiber.StatusFound ).To( "/account" )
		return
	} )
	app.Get( "/admin/logout" , handlers.page( "logout.html" ) )

	staff := handlers.Guard.RequireLoginPage
	app.Get( "/admin" , staff , handlers.page( "admin.html" ) )
	app.Get( "/admin/users" , staff , handlers.page( "admin_users.html" ) )
	app.Get( "/admin/users/:client_id" , staff , handlers.page( "admin_users.html" ) )
	app.Get( "/admin/barcodes" , staff , handlers.page( "admin_barcodes.html" ) )
	app.Get( "/admin/user/new" , staff , handlers.page( "admin_user_new.html" ) )
	app.Get( "/admin/checkin" , staff , handlers.page( "admin_checkin.html" ) )
	app.Get( "/admin/checkin/:client_id" , staff , handlers.page( "admin_checkin.html" ) )
	app.Get( "/admin/checkins" , staff , handlers.page( "admin_checkins.html" ) )
	app.Get( "/admin/checkins/day/:day" , staff , handlers.page( "admin_checkin_day.html" ) )
	app.Get( "/admin/settings" , staff , handlers.page( "settings.html" ) )
}

// Home sends staff to their dashboard and everyone else to staff sign-in.
// The public landing page and a family's phone pass are the public site's
// (cmd/site), not the station's.
func ( handlers *Handlers ) Home( c fiber.Ctx ) ( err error ) {
	target := "/account"
	if actor := handlers.actor( c ); actor != nil {
		target = "/admin"
	}
	err = c.Redirect().Status( fiber.StatusFound ).To( target )
	return
}

// ICal sends people to the closet's public calendar feed.
func ( handlers *Handlers ) ICal( c fiber.Ctx ) ( err error ) {
	target := handlers.Domain.Settings.Org().ICalURL
	if target == "" {
		err = notFound( c , "no calendar is configured" )
		return
	}
	err = c.Redirect().Status( fiber.StatusFound ).To( target )
	return
}
