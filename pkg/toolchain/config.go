package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Profile is intentionally an opaque command description. HikeC only expands
// input/output placeholders and executes the resulting argv.
type Profile struct {
	Command string
	Args    []string
}

type Config struct {
	Profiles map[string]Profile
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{Profiles: make(map[string]Profile)}
	section := ""
	var pendingArgs strings.Builder
	readingArgs := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if readingArgs {
			pendingArgs.WriteByte(' ')
			pendingArgs.WriteString(line)
			if strings.Contains(line, "]") {
				profile, ok := c.Profiles[section]
				if !ok {
					profile = Profile{}
				}
				profile.Args = parseStringArray(pendingArgs.String())
				c.Profiles[section] = profile
				readingArgs = false
				pendingArgs.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if !strings.HasPrefix(section, "profiles.") {
				section = ""
			}
			continue
		}
		if section == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid toolchain setting: %s", line)
		}
		profile := c.Profiles[section]
		switch strings.TrimSpace(key) {
		case "command":
			profile.Command = parseString(strings.TrimSpace(value))
		case "args":
			value = strings.TrimSpace(value)
			pendingArgs.WriteString(value)
			if strings.Contains(value, "]") {
				profile.Args = parseStringArray(pendingArgs.String())
				pendingArgs.Reset()
			} else {
				readingArgs = true
			}
		default:
			continue
		}
		c.Profiles[section] = profile
	}
	return c, nil
}

func LoadFromWorkingDirectory() (*Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(dir, "clang-target.toml")
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, os.ErrNotExist
		}
		dir = parent
	}
}

func (c *Config) Command(profileName, input, output string) (string, []string, bool) {
	profile, ok := c.Profiles["profiles."+profileName]
	if !ok || profile.Command == "" {
		return "", nil, false
	}
	expand := func(value string) string {
		value = strings.ReplaceAll(value, "{input}", input)
		return strings.ReplaceAll(value, "{output}", output)
	}
	args := make([]string, len(profile.Args))
	for i, arg := range profile.Args {
		args[i] = expand(arg)
	}
	return expand(profile.Command), args, true
}

func parseString(value string) string {
	// The source examples sometimes escape underscores for Markdown. TOML does
	// not require that escape, so accept both spellings here.
	value = strings.ReplaceAll(value, `\_`, `_`)
	if decoded, err := strconv.Unquote(value); err == nil {
		return decoded
	}
	return strings.Trim(value, `"`)
}

func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	if start := strings.IndexByte(value, '['); start >= 0 {
		value = value[start+1:]
	}
	if end := strings.LastIndexByte(value, ']'); end >= 0 {
		value = value[:end]
	}
	var result []string
	for len(value) > 0 {
		start := strings.IndexByte(value, '"')
		if start < 0 {
			break
		}
		value = value[start:]
		end := 1
		for end < len(value) {
			if value[end] == '"' && value[end-1] != '\\' {
				break
			}
			end++
		}
		if end >= len(value) {
			break
		}
		result = append(result, parseString(value[:end+1]))
		value = value[end+1:]
	}
	return result
}
