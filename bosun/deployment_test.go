package bosun

import (
	"encoding/xml"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	persistentStateContainerPath = "/var/lib/bosun"
	stateDirSetupCommand         = "install -d -o 1000 -g 1000 -m 0700 /mnt/user/appdata/bosun/state"
)

func TestContainerDeploymentsPersistState(t *testing.T) {
	t.Run("compose bind mount", func(t *testing.T) {
		data, err := os.ReadFile("docker-compose.yml")
		require.NoError(t, err)

		var compose struct {
			Services map[string]struct {
				Volumes []yaml.Node `yaml:"volumes"`
			} `yaml:"services"`
		}
		require.NoError(t, yaml.Unmarshal(data, &compose))

		bosun, ok := compose.Services["bosun"]
		require.True(t, ok, "compose file must define the bosun service")

		for i := range bosun.Volumes {
			if bosun.Volumes[i].Kind != yaml.MappingNode {
				continue
			}
			var volume struct {
				Type     string `yaml:"type"`
				Source   string `yaml:"source"`
				Target   string `yaml:"target"`
				ReadOnly *bool  `yaml:"read_only"`
				Bind     struct {
					CreateHostPath *bool `yaml:"create_host_path"`
				} `yaml:"bind"`
			}
			require.NoError(t, bosun.Volumes[i].Decode(&volume))
			if volume.Target != persistentStateContainerPath {
				continue
			}
			assert.Equal(t, "bind", volume.Type)
			assert.Equal(t, "/mnt/user/appdata/bosun/state", volume.Source)
			require.NotNil(t, volume.ReadOnly, "state bind must declare read_only explicitly")
			assert.False(t, *volume.ReadOnly)
			require.NotNil(t, volume.Bind.CreateHostPath, "state bind must declare create_host_path explicitly")
			assert.False(t, *volume.Bind.CreateHostPath, "Compose must not create the state directory as root")
			return
		}
		t.Fatalf("Compose service must bind %s", persistentStateContainerPath)
	})

	t.Run("unraid bind mount", func(t *testing.T) {
		data, err := os.ReadFile("../unraid-templates/bosun.xml")
		require.NoError(t, err)

		var template struct {
			Requires string `xml:"Requires"`
			Configs  []struct {
				Name        string `xml:"Name,attr"`
				Target      string `xml:"Target,attr"`
				Default     string `xml:"Default,attr"`
				Mode        string `xml:"Mode,attr"`
				Description string `xml:"Description,attr"`
				Required    string `xml:"Required,attr"`
			} `xml:"Config"`
		}
		require.NoError(t, xml.Unmarshal(data, &template))
		assert.Contains(t, template.Requires, stateDirSetupCommand)

		for _, config := range template.Configs {
			if config.Target != persistentStateContainerPath {
				continue
			}
			assert.Equal(t, "State Path", config.Name)
			assert.Equal(t, "/mnt/user/appdata/bosun/state", config.Default)
			assert.Equal(t, "rw", config.Mode)
			assert.Equal(t, "true", config.Required)
			assert.Contains(t, config.Description, stateDirSetupCommand)
			return
		}
		t.Fatalf("Unraid template must mount %s", persistentStateContainerPath)
	})

	t.Run("image directory ownership", func(t *testing.T) {
		data, err := os.ReadFile("Dockerfile")
		require.NoError(t, err)
		dockerfile := string(data)

		assert.Regexp(t, `(?m)^[[:space:]]*RUN addgroup -g 1000 bosun`, dockerfile)
		assert.Regexp(t, `(?m)^[[:space:]]*adduser -D -u 1000 -G bosun [^\n]*$`, dockerfile)
		assert.Regexp(t, `(?m)^[[:space:]]*mkdir -p [^\n]*/var/lib/bosun[^\n]*$`, dockerfile)
		assert.Regexp(t, `(?m)^[[:space:]]*chown -R bosun:bosun [^\n]*/var/lib/bosun[^\n]*$`, dockerfile)
		assert.Regexp(t, `(?m)^[[:space:]]*chmod 0700 /var/lib/bosun$`, dockerfile)
	})

	t.Run("operator documentation", func(t *testing.T) {
		files := []string{
			"../docs/gitops.md",
			"../docs/guides/unraid-setup.md",
			"../skills/onboard/resources/gitops.md",
			"../unraid-templates/README.md",
		}
		for _, path := range files {
			t.Run(path, func(t *testing.T) {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				content := string(data)
				assert.Contains(t, content, persistentStateContainerPath)
				assert.Contains(t, content, stateDirSetupCommand)
			})
		}
	})
}
