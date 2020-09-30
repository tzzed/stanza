package file

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/observiq/stanza/entry"
	"github.com/observiq/stanza/operator"
	"github.com/observiq/stanza/operator/helper"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

func init() {
	operator.RegisterOperator("file_input", func() operator.Builder { return NewInputConfig("") })
}

// NewInputConfig creates a new input config with default values
func NewInputConfig(operatorID string) *InputConfig {
	return &InputConfig{
		InputConfig:     helper.NewInputConfig(operatorID, "file_input"),
		PollInterval:    helper.Duration{Duration: 200 * time.Millisecond},
		IncludeFileName: true,
		IncludeFilePath: false,
		StartAt:         "end",
		MaxLogSize:      1024 * 1024,
		Encoding:        "nop",
	}
}

// InputConfig is the configuration of a file input operator
type InputConfig struct {
	helper.InputConfig `yaml:",inline"`

	Include []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`

	PollInterval    helper.Duration  `json:"poll_interval,omitempty"     yaml:"poll_interval,omitempty"`
	Multiline       *MultilineConfig `json:"multiline,omitempty"         yaml:"multiline,omitempty"`
	IncludeFileName bool             `json:"include_file_name,omitempty" yaml:"include_file_name,omitempty"`
	IncludeFilePath bool             `json:"include_file_path,omitempty" yaml:"include_file_path,omitempty"`
	StartAt         string           `json:"start_at,omitempty"          yaml:"start_at,omitempty"`
	MaxLogSize      int              `json:"max_log_size,omitempty"      yaml:"max_log_size,omitempty"`
	Encoding        string           `json:"encoding,omitempty"          yaml:"encoding,omitempty"`
}

// MultilineConfig is the configuration a multiline operation
type MultilineConfig struct {
	LineStartPattern string `json:"line_start_pattern" yaml:"line_start_pattern"`
	LineEndPattern   string `json:"line_end_pattern"   yaml:"line_end_pattern"`
}

// Build will build a file input operator from the supplied configuration
func (c InputConfig) Build(context operator.BuildContext) (operator.Operator, error) {
	inputOperator, err := c.InputConfig.Build(context)
	if err != nil {
		return nil, err
	}

	if len(c.Include) == 0 {
		return nil, fmt.Errorf("required argument `include` is empty")
	}

	// Ensure includes can be parsed as globs
	for _, include := range c.Include {
		_, err := filepath.Match(include, "matchstring")
		if err != nil {
			return nil, fmt.Errorf("parse include glob: %s", err)
		}
	}

	// Ensure excludes can be parsed as globs
	for _, exclude := range c.Exclude {
		_, err := filepath.Match(exclude, "matchstring")
		if err != nil {
			return nil, fmt.Errorf("parse exclude glob: %s", err)
		}
	}

	encoding, err := lookupEncoding(c.Encoding)
	if err != nil {
		return nil, err
	}

	splitFunc, err := c.getSplitFunc(encoding)
	if err != nil {
		return nil, err
	}

	var startAtBeginning bool
	switch c.StartAt {
	case "beginning":
		startAtBeginning = true
	case "end":
		startAtBeginning = false
	default:
		return nil, fmt.Errorf("invalid start_at location '%s'", c.StartAt)
	}

	fileNameField := entry.NewNilField()
	if c.IncludeFileName {
		fileNameField = entry.NewLabelField("file_name")
	}

	filePathField := entry.NewNilField()
	if c.IncludeFilePath {
		filePathField = entry.NewLabelField("file_path")
	}

	operator := &InputOperator{
		InputOperator:    inputOperator,
		Include:          c.Include,
		Exclude:          c.Exclude,
		SplitFunc:        splitFunc,
		PollInterval:     c.PollInterval.Raw(),
		persist:          helper.NewScopedDBPersister(context.Database, c.ID()),
		FilePathField:    filePathField,
		FileNameField:    fileNameField,
		fingerprintBytes: 1000,
		startAtBeginning: startAtBeginning,
		encoding:         encoding,
		firstCheck:       true,
		cancel:           func() {},
		knownFiles:       make([]*Reader, 0, 10),
		MaxLogSize:       c.MaxLogSize,
	}

	return operator, nil
}

var encodingOverrides = map[string]encoding.Encoding{
	"utf-16":   unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
	"utf16":    unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
	"utf8":     unicode.UTF8,
	"ascii":    unicode.UTF8,
	"us-ascii": unicode.UTF8,
	"nop":      encoding.Nop,
	"":         encoding.Nop,
}

func lookupEncoding(enc string) (encoding.Encoding, error) {
	if encoding, ok := encodingOverrides[strings.ToLower(enc)]; ok {
		return encoding, nil
	}
	encoding, err := ianaindex.IANA.Encoding(enc)
	if err != nil {
		return nil, fmt.Errorf("unsupported encoding '%s'", enc)
	}
	if encoding == nil {
		return nil, fmt.Errorf("no charmap defined for encoding '%s'", enc)
	}
	return encoding, nil
}

// getSplitFunc will return the split function associated the configured mode.
func (c InputConfig) getSplitFunc(encoding encoding.Encoding) (bufio.SplitFunc, error) {
	if c.Multiline == nil {
		return NewNewlineSplitFunc(encoding)
	}
	endPattern := c.Multiline.LineEndPattern
	startPattern := c.Multiline.LineStartPattern

	switch {
	case endPattern != "" && startPattern != "":
		return nil, fmt.Errorf("only one of line_start_pattern or line_end_pattern can be set")
	case endPattern == "" && startPattern == "":
		return nil, fmt.Errorf("one of line_start_pattern or line_end_pattern must be set")
	case endPattern != "":
		re, err := regexp.Compile("(?m)" + c.Multiline.LineEndPattern)
		if err != nil {
			return nil, fmt.Errorf("compile line end regex: %s", err)
		}
		return NewLineEndSplitFunc(re), nil
	case startPattern != "":
		re, err := regexp.Compile("(?m)" + c.Multiline.LineStartPattern)
		if err != nil {
			return nil, fmt.Errorf("compile line start regex: %s", err)
		}
		return NewLineStartSplitFunc(re), nil
	default:
		return nil, fmt.Errorf("unreachable")
	}
}
