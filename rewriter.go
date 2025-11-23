package switchfs

import (
	"regexp"
	"strings"
)

// prefixRewriter adds or removes a prefix from paths
type prefixRewriter struct {
	oldPrefix string
	newPrefix string
}

func (r *prefixRewriter) Rewrite(path string) string {
	if r.oldPrefix == "" {
		// Just add the new prefix
		return r.newPrefix + path
	}

	// Remove old prefix and add new prefix
	if strings.HasPrefix(path, r.oldPrefix) {
		path = strings.TrimPrefix(path, r.oldPrefix)
		return r.newPrefix + path
	}

	return path
}

// StripPrefix creates a rewriter that removes a prefix from paths
func StripPrefix(prefix string) PathRewriter {
	return &prefixRewriter{oldPrefix: prefix, newPrefix: ""}
}

// AddPrefix creates a rewriter that adds a prefix to paths
func AddPrefix(prefix string) PathRewriter {
	return &prefixRewriter{oldPrefix: "", newPrefix: prefix}
}

// ReplacePrefix creates a rewriter that replaces one prefix with another
func ReplacePrefix(oldPrefix, newPrefix string) PathRewriter {
	return &prefixRewriter{oldPrefix: oldPrefix, newPrefix: newPrefix}
}

// regexRewriter uses regex to rewrite paths
type regexRewriter struct {
	pattern     *regexp.Regexp
	replacement string
}

func (r *regexRewriter) Rewrite(path string) string {
	return r.pattern.ReplaceAllString(path, r.replacement)
}

// RegexRewrite creates a rewriter that uses regex patterns
func RegexRewrite(pattern, replacement string) (PathRewriter, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &regexRewriter{pattern: re, replacement: replacement}, nil
}

// chainRewriter applies multiple rewriters in sequence
type chainRewriter struct {
	rewriters []PathRewriter
}

func (r *chainRewriter) Rewrite(path string) string {
	for _, rewriter := range r.rewriters {
		path = rewriter.Rewrite(path)
	}
	return path
}

// ChainRewriters creates a rewriter that applies multiple rewriters in order
func ChainRewriters(rewriters ...PathRewriter) PathRewriter {
	return &chainRewriter{rewriters: rewriters}
}

// staticRewriter maps specific paths to new paths
type staticRewriter struct {
	mapping map[string]string
}

func (r *staticRewriter) Rewrite(path string) string {
	if newPath, ok := r.mapping[path]; ok {
		return newPath
	}
	return path
}

// StaticMapping creates a rewriter with a static path mapping
func StaticMapping(mapping map[string]string) PathRewriter {
	return &staticRewriter{mapping: mapping}
}
