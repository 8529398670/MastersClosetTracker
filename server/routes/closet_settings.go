package routes

import (
	errors "errors"

	fiber "github.com/gofiber/fiber/v3"

	printer "github.com/0187773933/MastersClosetTracker/server/printer"
	settings "github.com/0187773933/MastersClosetTracker/server/settings"
)

// The Settings page, the printer's language list, and the family-size cap the
// forms enforce.

// GetSettings is every field and its value. Any signed-in volunteer may look;
// only an admin may save. Secrets are never among the values.
func ( handlers *Handlers ) GetSettings( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{
		"fields": handlers.Domain.Settings.Fields(),
		"values": handlers.Domain.Settings.Values(),
	} )
	return
}

// UpdateSettings applies the Settings page (admin only).
func ( handlers *Handlers ) UpdateSettings( c fiber.Ctx ) ( err error ) {
	var edits map[string]any
	if c.Bind().JSON( &edits ) != nil {
		err = c.Status( fiber.StatusBadRequest ).JSON( fiber.Map{ "ok": false , "error": "invalid body" } )
		return
	}
	restart , update_err := handlers.Domain.Settings.Update( edits )
	if update_err != nil {
		var field_error *settings.FieldError
		if errors.As( update_err , &field_error ) {
			err = c.Status( fiber.StatusBadRequest ).JSON( fiber.Map{ "ok": false , "error": field_error.Error() } )
			return
		}
		err = serverError( c )
		return
	}
	err = c.JSON( fiber.Map{ "ok": true , "restart_required": restart } )
	return
}

func ( handlers *Handlers ) PrintLanguages( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{ "result": printer.Languages() } )
	return
}

// PrintStrings is every ticket language with the exact phrases the printer
// uses, so the on-screen preview matches the printed ticket.
func ( handlers *Handlers ) PrintStrings( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{ "result": printer.LanguageInfos() } )
	return
}

func ( handlers *Handlers ) Limits( c fiber.Ctx ) ( err error ) {
	err = c.JSON( fiber.Map{ "result": fiber.Map{ "max_family_size": handlers.Domain.Settings.Org().MaxFamilySize } } )
	return
}
