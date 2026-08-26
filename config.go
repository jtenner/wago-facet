package facet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const defaultMaxHandles uint32 = 1024

type Preopen struct {
	Guest  string
	Host   string
	Rights uint64
}

type Config struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Args           []string
	Env            []string
	Preopens       []Preopen
	MaxHandles     uint32
}

type pluginPreopen struct {
	Guest  string    `json:"guest"`
	Host   string    `json:"host"`
	Rights *[]string `json:"rights,omitempty"`
}

type pluginConfig struct {
	Stdin      string          `json:"stdin,omitempty"`
	Stdout     string          `json:"stdout,omitempty"`
	Stderr     string          `json:"stderr,omitempty"`
	Env        []string        `json:"env,omitempty"`
	Preopens   []pluginPreopen `json:"preopens,omitempty"`
	MaxHandles uint32          `json:"maxHandles,omitempty"`
}

var configSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "stdin": {"type": "string", "enum": ["inherit", "eof"]},
    "stdout": {"type": "string", "enum": ["inherit", "discard"]},
    "stderr": {"type": "string", "enum": ["inherit", "discard"]},
    "env": {
      "type": "array",
      "maxItems": 4096,
      "items": {"type": "string", "minLength": 2, "maxLength": 32768, "pattern": "^[^=\\u0000]+=[^\\u0000]*$"}
    },
    "preopens": {
      "type": "array",
      "maxItems": 64,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["guest", "host"],
        "properties": {
          "guest": {"type": "string", "minLength": 1, "maxLength": 4096, "pattern": "^[^\\u0000]+$"},
          "host": {"type": "string", "minLength": 1, "maxLength": 4096},
          "rights": {
            "type": "array",
            "uniqueItems": true,
            "items": {"type": "string", "enum": [
              "read", "write", "seek", "tell", "stat", "set-size", "sync",
              "path-open", "path-create", "path-remove", "path-rename", "path-link",
              "path-symlink", "path-readlink", "dir-iterate"
            ]}
          }
        }
      }
    },
    "maxHandles": {"type": "integer", "minimum": 8, "maximum": 65536}
  }
}`)

func ConfigSchema() json.RawMessage { return append(json.RawMessage(nil), configSchema...) }

func validatePluginConfig(raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = []byte("{}")
	}
	var cfg pluginConfig
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("plugin config contains multiple JSON values")
		}
		return err
	}
	return validatePluginConfigValue(cfg)
}

func validatePluginConfigValue(cfg pluginConfig) error {
	for field, value := range map[string]string{"stdin": cfg.Stdin, "stdout": cfg.Stdout, "stderr": cfg.Stderr} {
		if value == "" {
			continue
		}
		allowed := value == "inherit"
		if field == "stdin" {
			allowed = allowed || value == "eof"
		} else {
			allowed = allowed || value == "discard"
		}
		if !allowed {
			return fmt.Errorf("%s mode %q is invalid", field, value)
		}
	}
	if cfg.MaxHandles != 0 && (cfg.MaxHandles < 8 || cfg.MaxHandles > 65536) {
		return fmt.Errorf("maxHandles must be in [8, 65536]")
	}
	if len(cfg.Preopens) > 64 {
		return fmt.Errorf("preopens must contain at most 64 entries")
	}
	if len(cfg.Env) > 4096 {
		return fmt.Errorf("env must contain at most 4096 entries")
	}
	seenGuests := make(map[string]struct{}, len(cfg.Preopens))
	for i, p := range cfg.Preopens {
		guestLen := utf8.RuneCountInString(p.Guest)
		if guestLen < 1 || guestLen > 4096 || strings.IndexByte(p.Guest, 0) >= 0 {
			return fmt.Errorf("preopens[%d].guest is invalid", i)
		}
		hostLen := utf8.RuneCountInString(p.Host)
		if hostLen < 1 || hostLen > 4096 || strings.IndexByte(p.Host, 0) >= 0 {
			return fmt.Errorf("preopens[%d].host is invalid", i)
		}
		if _, ok := seenGuests[p.Guest]; ok {
			return fmt.Errorf("duplicate preopen guest name %q", p.Guest)
		}
		seenGuests[p.Guest] = struct{}{}
		if p.Rights != nil {
			seen := map[string]struct{}{}
			for _, name := range *p.Rights {
				if _, ok := rightByName(name); !ok {
					return fmt.Errorf("preopens[%d].rights contains unknown right %q", i, name)
				}
				if _, ok := seen[name]; ok {
					return fmt.Errorf("preopens[%d].rights repeats %q", i, name)
				}
				seen[name] = struct{}{}
			}
		}
	}
	for i, entry := range cfg.Env {
		units := utf8.RuneCountInString(entry)
		if units < 2 || units > 32768 {
			return fmt.Errorf("env[%d] length must be in [2, 32768]", i)
		}
		if strings.IndexByte(entry, 0) >= 0 {
			return fmt.Errorf("env[%d] contains NUL", i)
		}
		at := strings.IndexByte(entry, '=')
		if at <= 0 {
			return fmt.Errorf("env[%d] must be NAME=VALUE", i)
		}
	}
	return nil
}

func configFromPlugin(cfg pluginConfig) (Config, error) {
	if err := validatePluginConfigValue(cfg); err != nil {
		return Config{}, err
	}
	out := Config{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Env:        append([]string(nil), cfg.Env...),
		MaxHandles: cfg.MaxHandles,
	}
	if out.MaxHandles == 0 {
		out.MaxHandles = defaultMaxHandles
	}
	if cfg.Stdin == "eof" {
		out.Stdin = strings.NewReader("")
	}
	if cfg.Stdout == "discard" {
		out.Stdout = io.Discard
	}
	if cfg.Stderr == "discard" {
		out.Stderr = io.Discard
	}
	out.Preopens = make([]Preopen, 0, len(cfg.Preopens))
	for _, p := range cfg.Preopens {
		rights := defaultPreopenRights
		if p.Rights != nil {
			rights = 0
			for _, name := range *p.Rights {
				value, _ := rightByName(name)
				rights |= value
			}
		}
		out.Preopens = append(out.Preopens, Preopen{Guest: p.Guest, Host: p.Host, Rights: rights})
	}
	return normalizeConfig(out), nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.Stdin == nil {
		cfg.Stdin = strings.NewReader("")
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	if cfg.MaxHandles == 0 {
		cfg.MaxHandles = defaultMaxHandles
	}
	cfg.Args = append([]string(nil), cfg.Args...)
	cfg.Env = append([]string(nil), cfg.Env...)
	cfg.Preopens = append([]Preopen(nil), cfg.Preopens...)
	// Rights == 0 is a real zero-authority grant. Plugin JSON omission is
	// converted to defaultPreopenRights in configFromPlugin before this point.
	return cfg
}

func rightByName(name string) (uint64, bool) {
	switch name {
	case "read":
		return RightRead, true
	case "write":
		return RightWrite, true
	case "seek":
		return RightSeek, true
	case "tell":
		return RightTell, true
	case "stat":
		return RightStat, true
	case "set-size":
		return RightSetSize, true
	case "sync":
		return RightSync, true
	case "path-open":
		return RightPathOpen, true
	case "path-create":
		return RightPathCreate, true
	case "path-remove":
		return RightPathRemove, true
	case "path-rename":
		return RightPathRename, true
	case "path-link":
		return RightPathLink, true
	case "path-symlink":
		return RightPathSymlink, true
	case "path-readlink":
		return RightPathReadlink, true
	case "dir-iterate":
		return RightDirIterate, true
	default:
		return 0, false
	}
}
