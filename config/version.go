package config

var (
	Version    string = "dev"
	CommitHash string = "n/a"
)

func IsProduction() bool {
	return Version == "release"
}
