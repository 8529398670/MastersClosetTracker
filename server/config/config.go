// Package config is the single source of truth for everything this app can be
// told to do differently: the server, the Docker container, and the portable
// build. All of it lives in one config.yaml.
//
// Nothing reads environment variables. A setting that could come from the
// environment, a file, or a default has to be hunted for in three places;
// one that can only come from config.yaml is answered by reading one file.
// The shell scripts follow the same rule -- they ask this package for their
// values (`config shell`, see command.go) rather than parsing YAML themselves,
// so there is exactly one parser and one set of validation rules.
//
// Defaults are not written in Go. They live in defaults.yaml, which is
// embedded, documents every key, and is what `config init` writes out. A
// config.yaml is decoded on top of it, strictly: an unknown key is an error,
// because a typo that is silently ignored is a setting that silently is not
// applied.
package config

import (
	bytes "bytes"
	_ "embed"
	errors "errors"
	fmt "fmt"
	io "io"
	net "net"
	os "os"
	filepath "path/filepath"
	regexp "regexp"
	runtime "runtime"
	strings "strings"
	time "time"

	yaml "gopkg.in/yaml.v3"

	encryption "github.com/0187773933/MastersClosetTracker/server/encryption"
)

// AppSlug names the application on disk. It is what turns into
// ~/.config/<slug>/, so it is deliberately the lowercase, filesystem-safe
// form of the project name rather than the display name. It is fixed per
// binary (see Profile) rather than a setting because it is how config.yaml
// itself is found.
func AppSlug() ( slug string ) {
	slug = active.Slug
	return
}

// Filenames this app uses. Everything it persists lives under the app
// directory -- see DefaultAppDir for why.
// ConfigFileName is the name inside the app directory. In a source checkout
// the file is Profile.LocalFileName instead, so a station and a hub config can
// sit side by side.
const (
	ConfigFileName    = "config.yaml"
	DatabaseFileName  = "app.db"
	ControlSocketName = "control.sock"
	StorageDirName    = "storage"
	LanguageFileName  = "language.yaml"
)

// exampleHeader is what turns defaults.yaml into config.example.yaml: a note
// on how to copy it into place. See ExampleFileContent.
//
//go:embed example_header.yaml
var exampleHeader []byte


// ---------------------------------------------------------------------------
// config.yaml, section by section
// ---------------------------------------------------------------------------

// File mirrors config.yaml. Every section is decoded strictly, including the
// docker and build sections the server itself never reads: validating them
// here is what lets dockerRun.sh and build.sh fail on a typo before they
// start anything.
//
// The struct holds the sections of both profiles; Profile.Sections decides
// which ones a given file may contain, and Validate only checks those.
type File struct {
	App    AppSection    `yaml:"app"`
	Server ServerSection `yaml:"server"`
	Auth   AuthSection   `yaml:"auth"`
	Guests GuestsSection `yaml:"guests"`
	Sync   SyncSection   `yaml:"sync"`
	Hub    HubSection    `yaml:"hub"`
	Site   SiteSection   `yaml:"site"`
	Docker DockerSection `yaml:"docker"`
	Build  BuildSection  `yaml:"build"`
}

type AppSection struct {
	Name string `yaml:"name"`
}

type ServerSection struct {
	Host                   string `yaml:"host"`
	Port                   int    `yaml:"port"`
	AppDir                 string `yaml:"app_dir"`
	StaticDir              string `yaml:"static_dir"`
	LanguageFile           string `yaml:"language_file"`
	SecretKey              string `yaml:"secret_key"`
	EncryptAtRest          bool   `yaml:"encrypt_at_rest"`
	SecureCookies          bool   `yaml:"secure_cookies"`
	CookieName             string `yaml:"cookie_name"`
	TrustProxy             bool     `yaml:"trust_proxy"`
	TrustedProxies         []string `yaml:"trusted_proxies"`
	ProxyHeader            string   `yaml:"proxy_header"`
	BodyLimitBytes         int    `yaml:"body_limit_bytes"`
	RateLimitMax           int    `yaml:"rate_limit_max"`
	RateLimitWindowSeconds int    `yaml:"rate_limit_window_seconds"`
	ControlSocket          bool   `yaml:"control_socket"`
}

type AuthSection struct {
	SessionTTLSeconds    int  `yaml:"session_ttl_seconds"`
	LoginTokenTTLSeconds int  `yaml:"login_token_ttl_seconds"`
	APIKeys              bool `yaml:"api_keys"`
	APIKeyTTLSeconds     int  `yaml:"api_key_ttl_seconds"`
}

// GuestsSection is the station's family-facing side: where the public site
// (cmd/site) lives, so tickets can link a phone to its offline pass.
type GuestsSection struct {
	PublicURL string `yaml:"public_url"`

	// Retired with the signed phone passes. Still accepted so a config.yaml
	// written before then keeps loading; a value here only earns a warning.
	SigningKey    string `yaml:"signing_key"`
	CookieName    string `yaml:"cookie_name"`
	ClaimTTLHours int    `yaml:"claim_ttl_hours"`
	PassTTLDays   int    `yaml:"pass_ttl_days"`
}

// retired names the guests keys a config.yaml still sets, for the warning.
func ( section GuestsSection ) retired() ( names []string ) {
	if section.SigningKey != "" { names = append( names , "signing_key" ) }
	if section.CookieName != "" { names = append( names , "cookie_name" ) }
	if section.ClaimTTLHours != 0 { names = append( names , "claim_ttl_hours" ) }
	if section.PassTTLDays != 0 { names = append( names , "pass_ttl_days" ) }
	return
}

// SiteSection is the public site's own settings (mct-site only).
type SiteSection struct {
	ICalURL string `yaml:"ical_url"`
}

// SyncSection points a station at its hub. An empty hub_url means this
// station keeps its data to itself.
type SyncSection struct {
	HubURL      string `yaml:"hub_url"`
	APIKey      string `yaml:"api_key"`
	NodeName    string `yaml:"node_name"`
	PollSeconds int    `yaml:"poll_seconds"`
}

// HubSection bounds what one station may send the hub in one go.
type HubSection struct {
	MaxPushBytes        int `yaml:"max_push_bytes"`
	MaxDocBytes         int `yaml:"max_doc_bytes"`
	MaxFutureSeconds    int `yaml:"max_future_seconds"`
	PullPageMax         int `yaml:"pull_page_max"`
	KeyRateLimitMax     int `yaml:"key_rate_limit_max"`
	KeyRateLimitSeconds int `yaml:"key_rate_limit_window_seconds"`
}

type DockerSection struct {
	Image       string `yaml:"image"`
	Container   string `yaml:"container"`
	BindAddress string `yaml:"bind_address"`
	HostPort    int    `yaml:"host_port"`
	DataDir     string `yaml:"data_dir"`
	Restart     string `yaml:"restart"`
	Memory      string `yaml:"memory"`
	CPUs        string `yaml:"cpus"`
	PidsLimit   int    `yaml:"pids_limit"`
	TmpfsSize   string `yaml:"tmpfs_size"`
}

type BuildSection struct {
	BinaryName string   `yaml:"binary_name"`
	OutputDir  string   `yaml:"output_dir"`
	Targets    []string `yaml:"targets"`
}

// ---------------------------------------------------------------------------
// the resolved configuration the server runs on
// ---------------------------------------------------------------------------

// Config is config.yaml after resolution: paths made absolute, seconds turned
// into durations, container overrides applied. The rest of the server reads
// this, never File, so a change to the file's shape stays inside this package.
type Config struct {
	ProjectName string

	// ConfigFile is the config.yaml this was loaded from. It also holds the
	// secret key, which is why it is reported wherever backups are mentioned.
	ConfigFile string

	// ConfigCreated is true when this run wrote ConfigFile because none
	// existed. The startup log says so, since a fresh key was generated.
	ConfigCreated bool

	// ConfigWarning is non-empty when ConfigFile is readable by other users.
	// It is a warning rather than an error so a misconfigured mode never
	// stops a server that was working, but it does get logged on every start.
	ConfigWarning string

	// AppDir is the one directory this application owns: the database, the
	// control socket, and any uploaded or generated files.
	//
	// Defaulting to ~/.config/<slug> rather than ./data matters for the
	// single-file build: a binary run from a Downloads folder, a USB stick,
	// or a different working directory each time still finds the same state.
	AppDir       string
	DatabasePath string

	// DatabaseNoSync skips bolt's fsync on commit. Tests only -- nothing in
	// config.yaml can set it, because a crash would lose committed writes.
	DatabaseNoSync bool

	// ControlSocketPath is the unix socket the running server listens on so
	// that other processes can act on the database without opening it.
	//
	// bolt allows exactly one process to hold the database file, so a second
	// process cannot simply open it while the server is up. Rather than
	// fighting that, the server owns the database and everything else --
	// the manage CLI, a cron job, a sidecar -- asks the server over this
	// socket. One process holds the secret key, and there is no second copy
	// of the auth checks to keep in step.
	//
	// server.control_socket: false switches it off. Then manage only works
	// while the server is stopped.
	ControlSocketPath    string
	ControlSocketEnabled bool

	// StorageDir is for application files -- uploads, exports, generated
	// artifacts. Nothing in the base template writes here; it exists so that
	// features added later have an obvious, backed-up place to put files
	// instead of inventing one.
	StorageDir string

	// StaticDir and LanguageFile are checked on disk first; if either is
	// missing, the copy embedded in the binary is used instead (see
	// server/assets). That is what lets one build serve a live directory
	// during development and run as a single self-contained file in
	// production, with no flag to remember.
	StaticDir    string
	LanguageFile string

	Host string
	Port int

	// SecureCookies must stay true anywhere a real browser will talk to this
	// over the internet. It is only false for plain-http://localhost work,
	// because a Secure cookie is silently dropped over http and the login
	// flow would appear to succeed and then do nothing.
	SecureCookies bool
	CookieName    string

	SessionTTL    time.Duration
	LoginTokenTTL time.Duration

	// APIKeysEnabled decides whether the Authorization: Bearer header is
	// believed at all, and whether the endpoints that mint keys exist.
	// Turning it off stops existing keys from authenticating too -- the rows
	// stay, so switching it back on restores them.
	APIKeysEnabled bool

	// APIKeyTTL is how long a new key lasts when the request does not say.
	// The default is bounded so that a forgotten key eventually stops being
	// a live credential.
	APIKeyTTL time.Duration

	// SecretKey encrypts session cookie payloads and, when EncryptAtRest is
	// on, every value written to bolt. Losing it means losing the database
	// contents.
	SecretKey     string
	EncryptAtRest bool

	RateLimitMax    int
	RateLimitWindow time.Duration
	BodyLimit       int

	// TrustProxy tells fiber to believe ProxyHeader -- but only on a
	// connection whose peer address is in TrustedProxies. Fiber with
	// TrustProxy on and no allowlist trusts nobody, so every visitor behind
	// the proxy shares the proxy's one rate-limit budget; with the allowlist
	// and nothing else, any client could still name its own IP. Both halves
	// are required, and Validate refuses one without the other.
	TrustProxy     bool
	TrustedProxies []string
	ProxyHeader    string

	// SitePublicURL (station only) is the public site's base address, e.g.
	// https://masterscloset.cc, with no trailing slash. Empty means tickets
	// carry no phone pass QR code.
	SitePublicURL string

	// SiteICalURL (site only) is where the public site's /ical sends people.
	SiteICalURL string

	// Sync (station only). SyncEnabled is false when no hub is configured.
	SyncEnabled  bool
	SyncHubURL   string
	SyncAPIKey   string
	SyncNodeName string
	SyncPoll     time.Duration

	// Hub (hub only).
	HubMaxPushBytes   int
	HubMaxDocBytes    int
	HubMaxFuture      time.Duration
	HubPullPageMax    int
	HubKeyRateMax     int
	HubKeyRateWindow  time.Duration
}

// layout is what the container image pins, regardless of config.yaml. See
// layout_container.go; outside the image every field is empty and nothing is
// pinned.
type layout struct {
	ConfigFile    string
	AppDir        string
	Host          string
	ControlSocket string
}

// InContainer reports whether this binary was built for the container image.
func InContainer() ( result bool ) {
	result = pinned.ConfigFile != ""
	return
}

// ---------------------------------------------------------------------------
// where things live
// ---------------------------------------------------------------------------

// DefaultAppDir is ~/.config/<slug>. Everything lands in one directory rather
// than being split across XDG's config/data/state roots: this app's state is
// small, and one directory to back up is worth more here than strict
// adherence to a layout designed for larger, more separable applications.
func DefaultAppDir() ( dir string ) {
	if home , err := os.UserHomeDir(); err == nil && home != "" {
		dir = filepath.Join( home , ".config" , AppSlug() )
		return
	}
	// No home directory at all (some minimal environments). Fall back to the
	// working directory rather than failing to start.
	dir = filepath.Join( "." , AppSlug()+"-data" )
	return
}

// Locate finds the config file in use: the pinned path inside the container
// image, otherwise the file named by --config, otherwise the profile's local
// file (./config.yaml or ./config.hub.yaml), otherwise config.yaml in the
// default app directory. When none exists, path is where one would be
// created.
//
// ./config.yaml comes first because that is a source checkout, which is also
// where dockerRun.sh and build.sh read it. A portable binary run from
// anywhere else falls through to ~/.config/<slug>/ and finds the same file
// every time.
func Locate() ( path string , exists bool ) {
	if pinned.ConfigFile != "" {
		path = pinned.ConfigFile
		exists = isFile( path )
		return
	}
	if flagFile != "" {
		path = flagFile
		exists = isFile( path )
		return
	}
	if local , err := filepath.Abs( active.LocalFileName ); err == nil && isFile( local ) {
		path , exists = local , true
		return
	}
	path = filepath.Join( DefaultAppDir() , ConfigFileName )
	if absolute , err := filepath.Abs( path ); err == nil {
		path = absolute
	}
	exists = isFile( path )
	return
}

func isFile( path string ) ( result bool ) {
	info , err := os.Stat( path )
	result = err == nil && info.Mode().IsRegular()
	return
}

// resolvePath makes a config path absolute against the directory holding
// config.yaml, so the same file means the same thing whatever directory the
// process was started from.
func resolvePath( base string , value string ) ( result string ) {
	result = value
	if result == "" || filepath.IsAbs( result ) {
		return
	}
	result = filepath.Join( base , result )
	return
}

// ---------------------------------------------------------------------------
// reading and validating
// ---------------------------------------------------------------------------

func decodeStrict( raw []byte , out *File ) ( err error ) {
	err = decodeStrictAs( active , raw , out )
	return
}

// decodeStrictAs refuses unknown keys (yaml KnownFields) and, before that,
// any top-level section the profile does not have -- the struct knows both
// profiles' sections, so KnownFields alone would accept a hub section in a
// station's file.
func decodeStrictAs( profile *Profile , raw []byte , out *File ) ( err error ) {
	var root yaml.Node
	if err = yaml.Unmarshal( raw , &root ); err != nil { return }
	if len( root.Content ) > 0 && root.Content[ 0 ].Kind == yaml.MappingNode {
		mapping := root.Content[ 0 ]
		for index := 0; index+1 < len( mapping.Content ); index += 2 {
			key := mapping.Content[ index ].Value
			if profile.has( key ) == false {
				err = fmt.Errorf( "line %d: section %q does not belong in a %s config (allowed: %s)" ,
					mapping.Content[ index ].Line , key , profile.Name , strings.Join( profile.Sections , ", " ) )
				return
			}
		}
	}
	decoder := yaml.NewDecoder( bytes.NewReader( raw ) )
	decoder.KnownFields( true )
	err = decoder.Decode( out )
	if errors.Is( err , io.EOF ) {
		// An empty file is a valid file that sets nothing.
		err = nil
	}
	return
}

// Defaults is defaults.yaml on its own: the configuration a missing key falls
// back to.
func Defaults() ( file *File , err error ) {
	file , err = defaultsAs( active )
	return
}

func defaultsAs( profile *Profile ) ( file *File , err error ) {
	file = &File{}
	if err = decodeStrictAs( profile , profile.Defaults , file ); err != nil {
		err = fmt.Errorf( "embedded defaults for %s are invalid: %w" , profile.Name , err )
	}
	return
}

// Parse decodes a config.yaml over the defaults and validates the result.
// It does not require a secret key -- build.sh reads the build section of a
// checkout that may not have one yet. Load and `config check` do.
func Parse( raw []byte , label string ) ( file *File , err error ) {
	file , err = ParseAs( active , raw , label )
	return
}

// ParseAs is Parse for a profile other than this process's own -- `config
// check` uses it to read the sibling config and refuse collisions.
func ParseAs( profile *Profile , raw []byte , label string ) ( file *File , err error ) {
	file , err = defaultsAs( profile )
	if err != nil { return }
	if err = decodeStrictAs( profile , raw , file ); err != nil {
		err = fmt.Errorf( "%s: %w" , label , err )
		return
	}
	if err = file.validateAs( profile ); err != nil {
		err = fmt.Errorf( "%s: %w" , label , err )
	}
	return
}

// ReadFile reads and validates one config.yaml without touching anything else
// on disk.
func ReadFile( path string ) ( file *File , err error ) {
	raw , err := os.ReadFile( path )
	if err != nil { return }
	file , err = Parse( raw , path )
	return
}

// Current is the file the server would use, or the defaults when there is no
// file yet. It never writes anything, which is what the shell-facing commands
// and the healthcheck need.
func Current() ( file *File , path string , exists bool , err error ) {
	path , exists = Locate()
	if exists {
		file , err = ReadFile( path )
		return
	}
	file , err = Defaults()
	return
}

var (
	imagePattern     = regexp.MustCompile( `^[a-z0-9]([a-z0-9._-]*[a-z0-9])?(:[0-9]+)?(/[a-z0-9]([a-z0-9._-]*[a-z0-9])?)*$` )
	containerPattern = regexp.MustCompile( `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$` )
	restartPattern   = regexp.MustCompile( `^(no|always|unless-stopped|on-failure(:[0-9]+)?)$` )
	sizePattern      = regexp.MustCompile( `^[0-9]+[bkmgBKMG]?$` )
	cpusPattern      = regexp.MustCompile( `^[0-9]+(\.[0-9]+)?$` )
	targetPattern    = regexp.MustCompile( `^[a-z0-9]+/[a-z0-9]+$` )
	binaryPattern    = regexp.MustCompile( `^[A-Za-z0-9][A-Za-z0-9._-]*$` )
	secretPattern    = regexp.MustCompile( `^[0-9a-fA-F]{64}$` )

	httpURLPattern = regexp.MustCompile( `^https?://[^/\s?#]+$` )
)

// Validate rejects values that would fail later in a confusing way -- a port
// out of range, a Docker size the daemon would refuse, a build target go
// cannot parse. The docker and build values also end up in shell commands,
// so they are held to strict patterns rather than trusted.
func ( file *File ) Validate() ( err error ) {
	err = file.validateAs( active )
	return
}

func ( file *File ) validateAs( profile *Profile ) ( err error ) {
	var problems []string
	check := func( ok bool , format string , values ...any ) {
		if ok == false {
			problems = append( problems , fmt.Sprintf( format , values... ) )
		}
	}
	path_ok := func( value string ) ( result bool ) {
		result = strings.TrimSpace( value ) != "" && strings.ContainsAny( value , ":\n\r" ) == false
		return
	}

	check( strings.TrimSpace( file.App.Name ) != "" , "app.name must not be empty" )

	server := file.Server
	check( strings.TrimSpace( server.Host ) != "" , "server.host must not be empty" )
	check( server.Port >= 1 && server.Port <= 65535 , "server.port must be 1-65535, got %d" , server.Port )
	check( strings.TrimSpace( server.CookieName ) != "" , "server.cookie_name must not be empty" )
	check( strings.TrimSpace( server.ProxyHeader ) != "" , "server.proxy_header must not be empty" )
	check( server.TrustProxy == false || len( server.TrustedProxies ) > 0 ,
		"server.trust_proxy is true but server.trusted_proxies is empty -- list the address or CIDR range your reverse proxy connects from" )
	for _ , entry := range server.TrustedProxies {
		check( validProxyEntry( entry ) , "server.trusted_proxies entry %q must be an IP address or a CIDR range like 10.0.0.0/8" , entry )
		check( entry != "0.0.0.0/0" && entry != "::/0" , "server.trusted_proxies entry %q trusts every address, which lets any client forge its own IP" , entry )
	}
	check( server.BodyLimitBytes > 0 , "server.body_limit_bytes must be positive" )
	check( server.RateLimitMax >= 0 , "server.rate_limit_max must be 0 (off) or positive" )
	check( server.RateLimitWindowSeconds > 0 , "server.rate_limit_window_seconds must be positive" )

	if profile.has( "auth" ) {
		auth := file.Auth
		check( auth.SessionTTLSeconds > 0 , "auth.session_ttl_seconds must be positive" )
		check( auth.LoginTokenTTLSeconds > 0 , "auth.login_token_ttl_seconds must be positive" )
		check( auth.APIKeyTTLSeconds > 0 , "auth.api_key_ttl_seconds must be positive" )
	}

	if profile.has( "guests" ) {
		guests := file.Guests
		check( guests.PublicURL == "" || httpURLPattern.MatchString( guests.PublicURL ) ,
			"guests.public_url must be empty or an http(s) URL with no path, got %q" , guests.PublicURL )
	}

	if profile.has( "site" ) {
		check( file.Site.ICalURL == "" || strings.HasPrefix( file.Site.ICalURL , "https://" ) || strings.HasPrefix( file.Site.ICalURL , "http://" ) ,
			"site.ical_url must be empty or an http(s) URL, got %q" , file.Site.ICalURL )
	}

	if profile.has( "sync" ) {
		sync := file.Sync
		check( sync.HubURL == "" || httpURLPattern.MatchString( sync.HubURL ) ,
			"sync.hub_url must be empty or an http(s) URL with no path, got %q" , sync.HubURL )
		check( sync.HubURL == "" || strings.TrimSpace( sync.APIKey ) != "" ,
			"sync.hub_url is set but sync.api_key is empty -- create one on the hub with `mct-hub keys create -name <station>`" )
		check( sync.PollSeconds >= 1 && sync.PollSeconds <= 3600 , "sync.poll_seconds must be 1-3600" )
	}

	if profile.has( "hub" ) {
		hub := file.Hub
		check( hub.MaxDocBytes > 0 , "hub.max_doc_bytes must be positive" )
		check( hub.MaxPushBytes >= hub.MaxDocBytes , "hub.max_push_bytes must be at least hub.max_doc_bytes" )
		check( server.BodyLimitBytes >= hub.MaxPushBytes+65536 ,
			"server.body_limit_bytes must be at least hub.max_push_bytes + 65536 (%d)" , hub.MaxPushBytes+65536 )
		check( hub.MaxFutureSeconds > 0 , "hub.max_future_seconds must be positive" )
		check( hub.PullPageMax > 0 , "hub.pull_page_max must be positive" )
		check( hub.KeyRateLimitMax >= 0 , "hub.key_rate_limit_max must be 0 (off) or positive" )
		check( hub.KeyRateLimitSeconds > 0 , "hub.key_rate_limit_window_seconds must be positive" )
	}

	docker := file.Docker
	check( imagePattern.MatchString( docker.Image ) , "docker.image %q is not a valid lowercase image name" , docker.Image )
	check( containerPattern.MatchString( docker.Container ) , "docker.container %q is not a valid container name" , docker.Container )
	check( validBindAddress( docker.BindAddress ) , "docker.bind_address %q must be an IP address" , docker.BindAddress )
	check( docker.HostPort >= 1 && docker.HostPort <= 65535 , "docker.host_port must be 1-65535, got %d" , docker.HostPort )
	check( path_ok( docker.DataDir ) , "docker.data_dir must be a path with no ':' in it" )
	check( restartPattern.MatchString( docker.Restart ) , "docker.restart must be no, always, unless-stopped or on-failure[:N], got %q" , docker.Restart )
	check( sizePattern.MatchString( docker.Memory ) , "docker.memory must look like 256m, got %q" , docker.Memory )
	check( cpusPattern.MatchString( docker.CPUs ) , "docker.cpus must be a number like 1 or 0.5, got %q" , docker.CPUs )
	check( docker.PidsLimit > 0 , "docker.pids_limit must be positive" )
	check( sizePattern.MatchString( docker.TmpfsSize ) , "docker.tmpfs_size must look like 16m, got %q" , docker.TmpfsSize )

	build := file.Build
	check( binaryPattern.MatchString( build.BinaryName ) , "build.binary_name %q must be a plain file name" , build.BinaryName )
	check( path_ok( build.OutputDir ) , "build.output_dir must be a path with no ':' in it" )
	check( len( build.Targets ) > 0 , "build.targets must list at least one os/arch" )
	for _ , target := range build.Targets {
		check( targetPattern.MatchString( target ) , "build.targets entry %q must look like linux/amd64" , target )
	}

	if len( problems ) > 0 {
		err = errors.New( strings.Join( problems , "; " ) )
	}
	return
}

func validProxyEntry( value string ) ( result bool ) {
	if strings.Contains( value , "/" ) {
		_ , _ , err := net.ParseCIDR( value )
		result = err == nil
		return
	}
	result = net.ParseIP( value ) != nil
	return
}

func validBindAddress( value string ) ( result bool ) {
	result = net.ParseIP( value ) != nil
	return
}

// CheckSecretKey is separate from Validate because only a process that will
// actually use the key needs one.
//
// An empty key is refused rather than filled in. A file that already exists
// was created with a key, so an empty one means it was removed -- and
// quietly generating a replacement would make every existing record
// permanently unreadable.
func CheckSecretKey( file *File , path string ) ( err error ) {
	key := strings.TrimSpace( file.Server.SecretKey )
	if key == "" {
		err = fmt.Errorf(
			"server.secret_key is empty in %s\n"+
				"  If this app already has data, put back the key it was written with -- a new key cannot read it.\n"+
				"  For a fresh install, generate one with: openssl rand -hex 32" , path )
		return
	}
	if secretPattern.MatchString( key ) == false {
		err = fmt.Errorf( "server.secret_key in %s must be 64 hex characters (32 bytes), got %d characters" , path , len( key ) )
		return
	}
	return
}

// permissionWarning flags a config.yaml anyone on the machine can read. The
// file holds the secret key, so that is everyone's copy of it.
func permissionWarning( path string ) ( warning string ) {
	if runtime.GOOS == "windows" || active.Stateless {
		return
	}
	info , err := os.Stat( path )
	if err != nil { return }
	if info.Mode().Perm() & 0o004 != 0 {
		warning = fmt.Sprintf( "%s is readable by every user (mode %04o) and holds the secret key -- chmod 600 it" , path , info.Mode().Perm() )
	}
	return
}

// ---------------------------------------------------------------------------
// creating a config.yaml
// ---------------------------------------------------------------------------

var secretKeyLine = regexp.MustCompile( `(?m)^(\s*secret_key:\s*)""` )

// NewFileContent is the profile's defaults with freshly generated keys -- the
// whole file, comments included, so the result documents itself. Each key
// line gets its own random value.
func NewFileContent() ( content []byte , err error ) {
	defaults := active.Defaults
	matches := secretKeyLine.FindAllIndex( defaults , -1 )
	if active.Stateless {
		content = append( []byte{} , defaults... )
		return
	}
	if len( matches ) != 1 {
		err = fmt.Errorf( "embedded defaults must contain exactly one `secret_key: \"\"` line, found %d" , len( matches ) )
		return
	}
	content = secretKeyLine.ReplaceAllFunc( defaults , func( line []byte ) ( replaced []byte ) {
		replaced = secretKeyLine.ReplaceAll( line , []byte( `${1}"` + encryption.GenerateKeyHex() + `"` ) )
		return
	} )
	return
}

// ExampleFileContent is defaults.yaml with a header explaining how to copy
// it into place, and the secret key left empty. It is generated rather than
// kept by hand so that it cannot drift from the defaults it documents, and
// the empty key is deliberate: a key in a committed file is everyone's key.
// A copy that never gets one fails at startup with the line to run (see
// CheckSecretKey) instead of running on a shared key.
func ExampleFileContent() ( content []byte , err error ) {
	defaults := active.Defaults
	if matches := secretKeyLine.FindAllIndex( defaults , -1 ); len( matches ) != 1 && active.Stateless == false {
		err = fmt.Errorf( "embedded defaults must contain exactly one `secret_key: \"\"` line, found %d" , len( matches ) )
		return
	}
	header := strings.NewReplacer(
		"{{EXAMPLE_FILE}}" , active.ExampleFileName ,
		"{{LOCAL_FILE}}" , active.LocalFileName ,
		"{{BINARY}}" , active.Binary ,
	).Replace( string( exampleHeader ) )
	content = append( []byte( header ) , defaults... )
	return
}

// WriteExample writes config.example.yaml. Unlike WriteNew it overwrites:
// the file holds no secret, and regenerating it is the point.
func WriteExample( path string ) ( err error ) {
	content , err := ExampleFileContent()
	if err != nil { return }
	err = os.WriteFile( path , content , 0o644 )
	return
}

// WriteNew creates path from NewFileContent, refusing to overwrite: an
// existing config.yaml holds a key that existing data depends on.
func WriteNew( path string ) ( err error ) {
	content , err := NewFileContent()
	if err != nil { return }
	// 0700 because the secret key lives in here.
	if err = os.MkdirAll( filepath.Dir( path ) , 0o700 ); err != nil {
		err = fmt.Errorf( "could not create %s: %w" , filepath.Dir( path ) , err )
		return
	}
	handle , err := os.OpenFile( path , os.O_WRONLY|os.O_CREATE|os.O_EXCL , 0o600 )
	if err != nil {
		err = fmt.Errorf( "could not create %s: %w" , path , err )
		return
	}
	defer handle.Close()
	_ , err = handle.Write( content )
	return
}

// ---------------------------------------------------------------------------

// Load finds config.yaml (creating it with a fresh key if there is none),
// validates it, and makes sure the app directory exists. The server and the
// manage CLI both start here.
func Load() ( cfg *Config , err error ) {
	path , exists := Locate()
	created := false
	if exists == false {
		if InContainer() {
			err = fmt.Errorf( "no config at %s -- dockerRun.sh mounts config.yaml there" , path )
			return
		}
		if err = WriteNew( path ); err != nil { return }
		created = true
	}

	file , err := ReadFile( path )
	if err != nil { return }
	if active.Stateless == false {
		if err = CheckSecretKey( file , path ); err != nil { return }
	}

	base := filepath.Dir( path )

	app_dir := DefaultAppDir()
	if flagFile != "" && path == flagFile {
		app_dir = flagDataDir( path )
	}
	if file.Server.AppDir != "" {
		app_dir = resolvePath( base , file.Server.AppDir )
	}
	if pinned.AppDir != "" {
		app_dir = pinned.AppDir
	}
	if absolute , abs_err := filepath.Abs( app_dir ); abs_err == nil {
		app_dir = absolute
	}

	// 0700 because the database lives in here.
	if mkdir_err := os.MkdirAll( app_dir , 0o700 ); mkdir_err != nil {
		err = fmt.Errorf( "could not create app directory %q: %w" , app_dir , mkdir_err )
		return
	}
	storage_dir := filepath.Join( app_dir , StorageDirName )
	if mkdir_err := os.MkdirAll( storage_dir , 0o700 ); mkdir_err != nil {
		err = fmt.Errorf( "could not create storage directory %q: %w" , storage_dir , mkdir_err )
		return
	}

	// An operator can drop a language.yaml into the app directory to reword
	// the UI without rebuilding. Failing that, the one beside config.yaml (a
	// source checkout) is used, and failing that the embedded copy.
	language_file := resolvePath( base , file.Server.LanguageFile )
	if language_file == "" {
		language_file = filepath.Join( app_dir , LanguageFileName )
		if isFile( language_file ) == false {
			language_file = filepath.Join( base , LanguageFileName )
		}
	}

	host := file.Server.Host
	if pinned.Host != "" {
		host = pinned.Host
	}

	cfg = &Config{
		ProjectName: file.App.Name,

		ConfigFile:    path,
		ConfigCreated: created,
		ConfigWarning: permissionWarning( path ),

		AppDir:       app_dir,
		DatabasePath: filepath.Join( app_dir , DatabaseFileName ),

		ControlSocketPath:    controlSocketPath( app_dir ),
		ControlSocketEnabled: file.Server.ControlSocket,
		StorageDir:           storage_dir,

		StaticDir:    resolvePath( base , file.Server.StaticDir ),
		LanguageFile: language_file,

		Host: host,
		Port: file.Server.Port,

		SecureCookies: file.Server.SecureCookies,
		CookieName:    file.Server.CookieName,

		SessionTTL:    seconds( file.Auth.SessionTTLSeconds ),
		LoginTokenTTL: seconds( file.Auth.LoginTokenTTLSeconds ),

		APIKeysEnabled: file.Auth.APIKeys,
		APIKeyTTL:      seconds( file.Auth.APIKeyTTLSeconds ),

		SecretKey:     strings.TrimSpace( file.Server.SecretKey ),
		EncryptAtRest: file.Server.EncryptAtRest,

		RateLimitMax:    file.Server.RateLimitMax,
		RateLimitWindow: seconds( file.Server.RateLimitWindowSeconds ),
		BodyLimit:       file.Server.BodyLimitBytes,

		TrustProxy:     file.Server.TrustProxy,
		TrustedProxies: file.Server.TrustedProxies,
		ProxyHeader:    file.Server.ProxyHeader,
	}

	if active.has( "guests" ) {
		cfg.SitePublicURL = strings.TrimRight( file.Guests.PublicURL , "/" )
		if retired := file.Guests.retired(); len( retired ) > 0 {
			note := fmt.Sprintf( "guests.%s in %s no longer do anything (phone passes are offline now) -- delete them" , strings.Join( retired , ", guests." ) , path )
			if cfg.ConfigWarning != "" {
				note = cfg.ConfigWarning + "; " + note
			}
			cfg.ConfigWarning = note
		}
	}
	if active.has( "site" ) {
		cfg.SiteICalURL = strings.TrimSpace( file.Site.ICalURL )
	}
	if active.has( "sync" ) {
		cfg.SyncHubURL = strings.TrimRight( file.Sync.HubURL , "/" )
		cfg.SyncEnabled = cfg.SyncHubURL != ""
		cfg.SyncAPIKey = strings.TrimSpace( file.Sync.APIKey )
		// Empty stays empty: the station works its own name out (settings'
		// StationName), since a shared config.yaml cannot name each machine.
		cfg.SyncNodeName = strings.TrimSpace( file.Sync.NodeName )
		cfg.SyncPoll = seconds( file.Sync.PollSeconds )
	}
	if active.has( "hub" ) {
		cfg.HubMaxPushBytes = file.Hub.MaxPushBytes
		cfg.HubMaxDocBytes = file.Hub.MaxDocBytes
		cfg.HubMaxFuture = seconds( file.Hub.MaxFutureSeconds )
		cfg.HubPullPageMax = file.Hub.PullPageMax
		cfg.HubKeyRateMax = file.Hub.KeyRateLimitMax
		cfg.HubKeyRateWindow = seconds( file.Hub.KeyRateLimitSeconds )
	}
	return
}

func controlSocketPath( app_dir string ) ( path string ) {
	path = filepath.Join( app_dir , ControlSocketName )
	if pinned.ControlSocket != "" {
		path = pinned.ControlSocket
	}
	return
}

func seconds( value int ) ( result time.Duration ) {
	result = time.Duration( value ) * time.Second
	return
}

func ( cfg *Config ) ListenAddress() ( result string ) {
	result = fmt.Sprintf( "%s:%d" , cfg.Host , cfg.Port )
	return
}
