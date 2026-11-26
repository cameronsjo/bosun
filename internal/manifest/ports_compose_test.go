package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestComposePortFile_Labels checks that the port reader accepts every labels
// form compose allows. ComposePortService used to declare labels as
// map[string]string, so a service with list-form labels made the whole file
// fail to parse and its ports were never registered.
func TestComposePortFile_Labels(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "list labels",
			input: `services:
  web:
    image: nginx
    labels:
      - traefik.enable=true
      - com.example.team=web
    ports:
      - "8080:80"
`,
		},
		{
			name: "map labels with a bool value",
			input: `services:
  web:
    image: nginx
    labels:
      traefik.enable: true
    ports:
      - "8080:80"
`,
		},
		{
			name: "null labels",
			input: `services:
  web:
    image: nginx
    labels:
    ports:
      - "8080:80"
`,
		},
		{
			name: "no labels",
			input: `services:
  web:
    image: nginx
    ports:
      - "8080:80"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var compose ComposePortFile
			require.NoError(t, yaml.Unmarshal([]byte(tt.input), &compose))

			web, ok := compose.Services["web"]
			require.True(t, ok)
			require.Len(t, web.Ports, 1)
			parsed := ParsePortEntry(web.Ports[0])
			require.Len(t, parsed, 1)
			assert.Equal(t, 8080, parsed[0].HostPort)
		})
	}
}
