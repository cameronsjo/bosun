package update

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type goreleaserContract struct {
	ProjectName string `yaml:"project_name"`
	Builds      []struct {
		GOOS   []string `yaml:"goos"`
		GOARCH []string `yaml:"goarch"`
	} `yaml:"builds"`
	Archives []struct {
		Formats      []string `yaml:"formats"`
		NameTemplate string   `yaml:"name_template"`
	} `yaml:"archives"`
	Checksum struct {
		NameTemplate string `yaml:"name_template"`
		Algorithm    string `yaml:"algorithm"`
	} `yaml:"checksum"`
}

func TestGoReleaserChecksumContract(t *testing.T) {
	configPath := filepath.Join("..", "..", ".goreleaser.yaml")
	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var config goreleaserContract
	require.NoError(t, yaml.Unmarshal(contents, &config))
	require.Len(t, config.Builds, 1)
	require.Len(t, config.Archives, 1)
	assert.Equal(t, "bosun", config.ProjectName)
	assert.ElementsMatch(t, []string{"darwin", "linux"}, config.Builds[0].GOOS)
	assert.ElementsMatch(t, []string{"amd64", "arm64"}, config.Builds[0].GOARCH)
	assert.Equal(t, []string{"tar.gz"}, config.Archives[0].Formats)
	assert.Equal(t, "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}", config.Archives[0].NameTemplate)
	assert.Equal(t, checksumAssetName, config.Checksum.NameTemplate)
	assert.Equal(t, "sha256", config.Checksum.Algorithm)

	var archives []string
	for _, osName := range config.Builds[0].GOOS {
		for _, archName := range config.Builds[0].GOARCH {
			archives = append(archives, fmt.Sprintf("bosun_1.2.3_%s_%s.tar.gz", osName, archName))
		}
	}
	sort.Strings(archives)
	assert.Equal(t, []string{
		"bosun_1.2.3_darwin_amd64.tar.gz",
		"bosun_1.2.3_darwin_arm64.tar.gz",
		"bosun_1.2.3_linux_amd64.tar.gz",
		"bosun_1.2.3_linux_arm64.tar.gz",
	}, archives)
}
