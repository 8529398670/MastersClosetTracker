package clients

// Similar reports the ways two clients look like the same family, for
// spotting a family that registered twice.
type Similar struct {
	Name     bool `json:"name"`
	Email    bool `json:"email"`
	Phone    bool `json:"phone"`
	Address  bool `json:"address"`
	Birthday bool `json:"birthday"`
	Barcode  bool `json:"barcode"`
}

func ( similar Similar ) Any() ( result bool ) {
	result = similar.Name || similar.Email || similar.Phone || similar.Address || similar.Birthday || similar.Barcode
	return
}

// Compare checks a against b. threshold is how many edits apart two names
// (or emails, or street details) may be and still count as alike.
func Compare( a *Client , b *Client , threshold int ) ( result Similar ) {
	within := func( x string , y string ) ( ok bool ) {
		if x == "" || y == "" { return }
		ok = Levenshtein( Fold( x ) , Fold( y ) ) < threshold
		return
	}
	result.Name = within( a.Identity.FirstName , b.Identity.FirstName ) && within( a.Identity.LastName , b.Identity.LastName )
	result.Email = within( a.EmailAddress , b.EmailAddress )
	result.Phone = a.PhoneNumber != "" && a.PhoneNumber == b.PhoneNumber
	result.Address = within( a.Identity.Address.StreetNumber , b.Identity.Address.StreetNumber ) &&
		within( a.Identity.Address.StreetName , b.Identity.Address.StreetName )
	birthday_a , birthday_b := a.Identity.DateOfBirth , b.Identity.DateOfBirth
	result.Birthday = birthday_a.Year != 0 && birthday_a.Month != "" && birthday_a.Day != 0 && birthday_a == birthday_b
	for _ , x := range a.Barcodes {
		for _ , y := range b.Barcodes {
			if x != "" && x == y {
				result.Barcode = true
			}
		}
	}
	return
}

// Levenshtein is the edit distance between two strings, by rune.
func Levenshtein( a string , b string ) ( distance int ) {
	x , y := []rune( a ) , []rune( b )
	previous := make( []int , len( y )+1 )
	current := make( []int , len( y )+1 )
	for j := range previous {
		previous[ j ] = j
	}
	for i := 1; i <= len( x ); i++ {
		current[ 0 ] = i
		for j := 1; j <= len( y ); j++ {
			cost := 1
			if x[ i-1 ] == y[ j-1 ] {
				cost = 0
			}
			current[ j ] = min( previous[ j ]+1 , current[ j-1 ]+1 , previous[ j-1 ]+cost )
		}
		previous , current = current , previous
	}
	distance = previous[ len( y ) ]
	return
}
