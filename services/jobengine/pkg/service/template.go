package service

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	templateRe    = regexp.MustCompile(`\{\{([^}]+)\}\}`)
	envRe         = regexp.MustCompile(`\$\{([^}]+)\}`)
	unsafeCharsRe = regexp.MustCompile(`[;&|$` + "`" + `\\\n\r!{}()<>]`)
)

// SanitizeValue removes characters that could be used for shell injection.
// Only applied to user-supplied values (names, paths), not to internal paths.
func SanitizeValue(val string) string {
	return unsafeCharsRe.ReplaceAllString(val, "_")
}

// TemplateVars holds all available variables for template resolution
type TemplateVars struct {
	Source      string
	SourceName string
	SourceExt  string
	Target     string
	TargetDir  string
	User       UserInfo
	Space      SpaceInfo
	Resource   ResourceInfo
	Options    map[string]string
}

type UserInfo struct {
	ID          string
	DisplayName string
	Email       string
}

type SpaceInfo struct {
	ID   string
	Name string
}

type ResourceInfo struct {
	ID   string
	Name string
	Path string
}

// Resolve replaces {{variables}} and ${ENV} in a string
func Resolve(tmpl string, vars *TemplateVars) string {
	result := templateRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		return resolveKey(key, vars)
	})

	result = envRe.ReplaceAllStringFunc(result, func(match string) string {
		inner := match[2 : len(match)-1]
		parts := strings.SplitN(inner, "|", 2)
		envKey := strings.TrimSpace(parts[0])
		val := os.Getenv(envKey)
		if val == "" && len(parts) > 1 {
			val = strings.TrimSpace(parts[1])
		}
		return val
	})

	return result
}

// ResolveArgs resolves a slice of template strings
func ResolveArgs(args []string, vars *TemplateVars) []string {
	resolved := make([]string, len(args))
	for i, a := range args {
		resolved[i] = Resolve(a, vars)
	}
	return resolved
}

func resolveKey(key string, vars *TemplateVars) string {
	switch key {
	case "source":
		return vars.Source
	case "target":
		return vars.Target
	case "target_dir":
		return vars.TargetDir
	case "source.name":
		return SanitizeValue(vars.SourceName)
	case "source.nameWithoutExt":
		if vars.SourceExt != "" {
			return SanitizeValue(strings.TrimSuffix(vars.SourceName, vars.SourceExt))
		}
		return SanitizeValue(vars.SourceName)
	case "source.ext":
		return SanitizeValue(vars.SourceExt)
	case "user.id":
		return SanitizeValue(vars.User.ID)
	case "user.displayName":
		return SanitizeValue(vars.User.DisplayName)
	case "user.email":
		return SanitizeValue(vars.User.Email)
	case "space.id":
		return SanitizeValue(vars.Space.ID)
	case "space.name":
		return SanitizeValue(vars.Space.Name)
	case "resource.name":
		return SanitizeValue(vars.Resource.Name)
	case "resource.path":
		return SanitizeValue(vars.Resource.Path)
	case "now":
		return time.Now().Format(time.RFC3339)
	case "now.date":
		return time.Now().Format("2006-01-02")
	default:
		if strings.HasPrefix(key, "options.") {
			optKey := key[8:]
			if v, ok := vars.Options[optKey]; ok {
				return v
			}
		}
		return fmt.Sprintf("{{%s}}", key)
	}
}
