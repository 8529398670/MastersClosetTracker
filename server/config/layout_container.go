//go:build container

package config

// pinned is the container image's fixed layout. Only the Dockerfile builds
// with -tags container, so this applies inside the image and nowhere else.
//
// These are not settings because they are not choices: they are the paths
// dockerRun.sh mounts into. Pinning them in the binary rather than passing
// them through ENV or flags means `docker exec <name> /app/server manage ...`
// finds the same config and database as the server with nothing extra to
// type, and a host-oriented server.app_dir in the shared config.yaml cannot
// point the container at a path that is not mounted.
//
//	/app/config.yaml   ./config.yaml, mounted read-only
//	/app/data          docker.data_dir, the only writable mount
//	0.0.0.0            inside the container's own network namespace; what is
//	                   reachable from outside is docker.bind_address
//
// The control socket lives on the container's own /tmp (a tmpfs), not in
// /app/data: a bind-mounted host directory may refuse sockets (Docker
// Desktop's file sharing on macOS does), and the socket only ever needs to
// be reached from inside the container (`docker exec ... manage`).
var pinned = layout{
	ConfigFile:    "/app/config.yaml",
	AppDir:        "/app/data",
	Host:          "0.0.0.0",
	ControlSocket: "/tmp/control.sock",
}
