package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is Pivot's complete runtime configuration.
//
// Defaults must be good enough that `./pivot serve` works with no file, no
// environment, and no flags — that is the 30-second-install commitment from
// docs/vision.md. Every field added here needs a default, a validation rule,
// and an entry in the environment binding table in env.go.
type Config struct {
	Server ServerConfig `yaml:"server"`
	Log    LogConfig    `yaml:"log"`
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	// Host to bind. Empty binds all interfaces.
	Host string `yaml:"host"`

	// Port to listen on.
	Port int `yaml:"port"`

	// ReadHeaderTimeout bounds how long a client may take to send headers.
	// This is the defense against Slowloris, so it is deliberately short and
	// separate from ReadTimeout.
	ReadHeaderTimeout Duration `yaml:"readHeaderTimeout"`

	// ReadTimeout bounds reading the entire request, body included.
	ReadTimeout Duration `yaml:"readTimeout"`

	// WriteTimeout bounds writing the response. Query results can be large and
	// slow, so this is generous; streaming endpoints manage their own deadlines.
	WriteTimeout Duration `yaml:"writeTimeout"`

	// IdleTimeout bounds how long a keep-alive connection may sit unused.
	IdleTimeout Duration `yaml:"idleTimeout"`

	// ShutdownTimeout bounds draining in-flight requests on SIGTERM before the
	// process exits anyway.
	ShutdownTimeout Duration `yaml:"shutdownTimeout"`

	// PreShutdownDelay is the "lame duck" period: on SIGTERM the server fails
	// its readiness probe, keeps serving for this long, and only then stops
	// accepting connections.
	//
	// Without it, failing readiness first accomplishes nothing — http.Shutdown
	// stops accepting immediately, so a load balancer polling /readyz gets a
	// connection refusal rather than a 503, and keeps routing to an instance
	// that is already gone.
	//
	// Zero by default so local Ctrl-C is instant. Behind a load balancer set
	// it above the readiness probe interval (5s suits Kubernetes defaults).
	PreShutdownDelay Duration `yaml:"preShutdownDelay"`
}

// LogConfig controls structured logging.
type LogConfig struct {
	// Level is one of debug, info, warn, error.
	Level string `yaml:"level"`

	// Format is json or text. JSON is the default because logs are read by
	// machines far more often than by people.
	Format string `yaml:"format"`

	// AddSource attaches the calling file and line. Useful when debugging,
	// costly in hot paths, so it is off by default.
	AddSource bool `yaml:"addSource"`
}

// Address returns the host:port the server binds.
func (s ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// Default returns the configuration used when nothing else is supplied.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host:              "",
			Port:              8080,
			ReadHeaderTimeout: Duration(5 * time.Second),
			ReadTimeout:       Duration(30 * time.Second),
			WriteTimeout:      Duration(120 * time.Second),
			IdleTimeout:       Duration(90 * time.Second),
			ShutdownTimeout:   Duration(30 * time.Second),
			PreShutdownDelay:  0,
		},
		Log: LogConfig{
			Level:     "info",
			Format:    "json",
			AddSource: false,
		},
	}
}

// Duration wraps [time.Duration] so configuration files can say "30s" instead
// of a nanosecond count. The zero value is a zero duration.
type Duration time.Duration

// Duration returns the underlying [time.Duration].
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String implements [fmt.Stringer].
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML accepts either a duration string ("30s") or a plain integer
// count of seconds, because both are things people reasonably write.
//
// The YAML tag is what distinguishes them. Decoding into a string first does
// not work: yaml.v3 will happily render the scalar 120 as "120", which then
// fails to parse as a duration.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!int" {
		var secs int64
		if err := node.Decode(&secs); err != nil {
			return fmt.Errorf("parse duration %q: %w", node.Value, err)
		}
		*d = Duration(time.Duration(secs) * time.Second)

		return nil
	}

	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf(
			"duration must be a string like %q or a number of seconds, got %q",
			"30s", node.Value,
		)
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)

	return nil
}

// MarshalYAML writes durations back in their human-readable form.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }
