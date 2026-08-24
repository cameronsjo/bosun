package bosun

import (
	"encoding/xml"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const persistentStateContainerPath = "/var/lib/bosun"

func TestContainerDeploymentsPersistState(t *testing.T) {
	t.Run("compose bind mount", func(t *testing.T) {
		data, err := os.ReadFile("docker-compose.yml")
		require.NoError(t, err)

		var compose struct {
			Services map[string]struct {
				Volumes []string `yaml:"volumes"`
			} `yaml:"services"`
		}
		require.NoError(t, yaml.Unmarshal(data, &compose))

		bosun, ok := compose.Services["bosun"]
		require.True(t, ok, "compose file must define the bosun service")
		assert.Contains(t, bosun.Volumes, "/mnt/user/appdata/bosun/state:"+persistentStateContainerPath+":rw")
	})

	t.Run("unraid bind mount", func(t *testing.T) {
		data, err := os.ReadFile("../unraid-templates/bosun.xml")
		require.NoError(t, err)

		var template struct {
			Configs []struct {
				Name     string `xml:"Name,attr"`
				Target   string `xml:"Target,attr"`
				Default  string `xml:"Default,attr"`
				Mode     string `xml:"Mode,attr"`
				Required string `xml:"Required,attr"`
			} `xml:"Config"`
		}
		require.NoError(t, xml.Unmarshal(data, &template))

		for _, config := range template.Configs {
			if config.Target != persistentStateContainerPath {
				continue
			}
			assert.Equal(t, "State Path", config.Name)
			assert.Equal(t, "/mnt/user/appdata/bosun/state", config.Default)
			assert.Equal(t, "rw", config.Mode)
			assert.Equal(t, "true", config.Required)
			return
		}
		t.Fatalf("Unraid template must mount %s", persistentStateContainerPath)
	})

	t.Run("image directory ownership", func(t *testing.T) {
		data, err := os.ReadFile("Dockerfile")
		require.NoError(t, err)
		dockerfile := string(data)

		assert.Regexp(t, `(?m)^[[:space:]]*mkdir -p [^\n]*/var/lib/bosun[^\n]*$`, dockerfile)
		assert.Regexp(t, `(?m)^[[:space:]]*chown -R bosun:bosun [^\n]*/var/lib/bosun[^\n]*$`, dockerfile)
	})
}
