package plugin

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/observiq/stanza/operator"
	"github.com/stretchr/testify/require"
)

func NewTempDir(t *testing.T) string {
	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})

	return tempDir
}

func TestNewRegistry(t *testing.T) {
	tempDir := NewTempDir(t)

	test1 := []byte(`
id: my_generator
type: generator
output: testoutput
record:
  message1: {{ .message }}
`)

	test2 := []byte(`
id: my_generator
type: generator
output: testoutput
record:
  message2: {{ .message }}
`)

	err := ioutil.WriteFile(filepath.Join(tempDir, "test1.yaml"), test1, 0666)
	require.NoError(t, err)
	err = ioutil.WriteFile(filepath.Join(tempDir, "test2.yaml"), test2, 0666)
	require.NoError(t, err)

	registry, err := NewPluginRegistry(tempDir)
	require.NoError(t, err)

	require.Equal(t, 2, len(registry))
	require.True(t, registry.IsDefined("test1"))
	require.True(t, registry.IsDefined("test2"))
}

func TestNewRegistryFailure(t *testing.T) {
	tempDir := NewTempDir(t)
	err := ioutil.WriteFile(filepath.Join(tempDir, "invalid.yaml"), []byte("pipeline:"), 0111)
	require.NoError(t, err)

	_, err = NewPluginRegistry(tempDir)
	require.Error(t, err)
}

func TestRegistryRender(t *testing.T) {
	t.Run("ErrorTypeDoesNotExist", func(t *testing.T) {
		reg := Registry{}
		_, err := reg.Render("unknown", map[string]interface{}{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})

	t.Run("ErrorExecFailure", func(t *testing.T) {
		tmpl, err := template.New("plugintype").Parse(`{{ .panicker }}`)
		require.NoError(t, err)

		reg := Registry{
			"plugintype": tmpl,
		}
		params := map[string]interface{}{
			"panicker": func() {
				panic("testpanic")
			},
		}
		_, err = reg.Render("plugintype", params)
		require.Contains(t, err.Error(), "failed to render")
	})
}

func TestRegistryLoad(t *testing.T) {
	t.Run("LoadAllBadGlob", func(t *testing.T) {
		reg := Registry{}
		err := reg.LoadAll("", `[]`)
		require.Error(t, err)
		require.Contains(t, err.Error(), "with glob pattern")
	})

	t.Run("AddDuplicate", func(t *testing.T) {
		reg := Registry{}
		operator.Register("copy", func() operator.Builder { return nil })
		err := reg.Add("copy", "pipeline:\n")
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")
	})

	t.Run("AddBadTemplate", func(t *testing.T) {
		reg := Registry{}
		err := reg.Add("new", "{{ nofunc }")
		require.Error(t, err)
		require.Contains(t, err.Error(), "as a plugin template")
	})

	t.Run("LoadAllWithFailures", func(t *testing.T) {
		tempDir := NewTempDir(t)
		pluginPath := filepath.Join(tempDir, "copy.yaml")
		err := ioutil.WriteFile(pluginPath, []byte("pipeline:\n"), 0755)
		require.NoError(t, err)

		reg := Registry{}
		err = reg.LoadAll(tempDir, "*.yaml")
		require.Error(t, err)
	})
}

func TestPluginMetadata(t *testing.T) {
	testCases := []struct {
		name      string
		expectErr bool
		template  string
	}{
		{
			name:      "no_meta",
			expectErr: false,
			template: `pipeline:
`,
		},
		{
			name:      "full_meta",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Path
    description: The path to a thing
    type: string
  other:
    label: Other Thing
    description: Another parameter
    type: int
pipeline:
`,
		},
		{
			name:      "bad_version",
			expectErr: true,
			template: `version: []
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Path
    description: The path to a thing
    type: string
  other:
    label: Other Thing
    description: Another parameter
    type: int
pipeline:
`,
		},
		{
			name:      "bad_title",
			expectErr: true,
			template: `version: 0.0.0
title: []
description: This is a test plugin
parameters:
  path:
    label: Path
    description: The path to a thing
    type: string
  other:
    label: Other Thing
    description: Another parameter
    type: int
pipeline:
`,
		},
		{
			name:      "bad_description",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: []
parameters:
  path:
    label: Path
    description: The path to a thing
    type: string
  other:
    label: Other Thing
    description: Another parameter
    type: int
pipeline:
`,
		},
		{
			name:      "bad_parameters",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters: hello
`,
		},
		{
			name:      "bad_parameter_structure",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path: this used to be supported
pipeline:
`,
		},
		{
			name:      "bad_parameter_label",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: []
    description: The path to a thing
    type: string
pipeline:
`,
		},
		{
			name:      "bad_parameter_description",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Path
    description: []
    type: string
pipeline:
`,
		},
		{
			name:      "bad_parameter",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Path
    description: The path to a thing
    type: {}
pipeline:
`,
		},
		{
			name:      "empty_parameter",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
pipeline:
`,
		},
		{
			name:      "unknown_parameter",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: custom
pipeline:
`,
		},
		{
			name:      "string_parameter",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: string
pipeline:
`,
		},
		{
			name:      "string_parameter_default",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: string
    default: hello
pipeline:
`,
		},
		{
			name:      "string_parameter_default_invalid",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: string
    default: 123
pipeline:
`,
		},
		{
			name:      "strings_parameter",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: strings
pipeline:
`,
		},
		{
			name:      "strings_parameter_default",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: strings
    default:
     - hello
pipeline:
`,
		},
		{
			name:      "strings_parameter_default_invalid",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: strings
    default: hello
pipeline:
`,
		},

		{
			name:      "int_parameter",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: int
pipeline:
`,
		},
		{
			name:      "int_parameter_default",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: int
    default: 123
pipeline:
`,
		},
		{
			name:      "int_parameter_default_invalid",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: int
    default: hello
pipeline:
`,
		},
		{
			name:      "bool_parameter",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: bool
pipeline:
`,
		},
		{
			name:      "bool_parameter_default_true",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: bool
    default: true
pipeline:
`,
		},
		{
			name:      "bool_parameter_default_false",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: bool
    default: false
pipeline:
`,
		},
		{
			name:      "bool_parameter_default_invalid",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: bool
    default: 123
pipeline:
`,
		},
		{
			name:      "enum_parameter",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: enum
    valid_values: ["one", "two"]
pipeline:
`,
		},
		{
			name:      "enum_parameter_alternate",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: enum
    valid_values:
     - one
     - two
pipeline:
`,
		},
		{
			name:      "enum_parameter_default",
			expectErr: false,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: enum
    valid_values:
     - one
     - two
    default: one
pipeline:
`,
		},
		{
			name:      "enum_parameter_default_invalid",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: enum
    valid_values:
     - one
     - two
    default: three
pipeline:
`,
		},
		{
			name:      "enum_parameter_no_valid_values",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: enum
pipeline:
`,
		},
		{
			name:      "default_invalid",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: int
    default: {}
pipeline:
`,
		},
		{
			name:      "required_default",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    required: true
    type: int
    default: 123
pipeline:
`,
		},
		{
			name:      "non_enum_valid_values",
			expectErr: true,
			template: `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: The thing of the thing
    type: int
    valid_values: [1, 2, 3]
pipeline:
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reg := Registry{}
			err := reg.Add(tc.name, tc.template)
			require.NoError(t, err)
			_, err = reg.Render(tc.name, map[string]interface{}{})
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRenderWithMissingRequired(t *testing.T) {
	template := `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: A parameter
    type: int
    required: true
pipeline:
`
	reg := Registry{}
	err := reg.Add("plugin", template)
	require.NoError(t, err)

	_, err = reg.Render("plugin", map[string]interface{}{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required parameter for plugin")
}

func TestRenderWithInvalidParameter(t *testing.T) {
	template := `version: 0.0.0
title: Test Plugin
description: This is a test plugin
parameters:
  path:
    label: Parameter
    description: A parameter
    type: int
    required: true
pipeline:
`
	reg := Registry{}
	err := reg.Add("plugin", template)
	require.NoError(t, err)

	_, err = reg.Render("plugin", map[string]interface{}{"path": "test"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "plugin parameter failed validation")
}

func TestDefaultPluginFuncWithValue(t *testing.T) {
	result := defaultPluginFunc("default_value", "supplied_value")
	require.Equal(t, "supplied_value", result)
}

func TestDefaultPluginFuncWithoutValue(t *testing.T) {
	result := defaultPluginFunc("default_value", nil)
	require.Equal(t, "default_value", result)
}
