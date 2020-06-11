package configs

import (
	"os"
	"path/filepath"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	TestDir string
	suite.Suite
}

func (suite *ConfigTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir
	CurrentConfigs = &Configs{}

	err := os.Mkdir(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}
}

func (suite *ConfigTestSuite) TestWriteToFile() {
	assert := assert2.New(suite.T())
	file := filepath.Join(suite.TestDir, Filename)

	// Write the configs for first time.
	err := WriteToFile()
	assert.NoError(err, "The configs must be written to the file correctly")
	assert.FileExistsf(file, "The file '%s' should exist on '%s'", Filename, suite.TestDir)

	// Modify some setting and try to write it to the file again.
	CurrentConfigs.Behavior.LoopSleep = 800
	err = WriteToFile()
	assert.NoError(err, "The configs must be written to the file correctly")
	assert.FileExistsf(file, "The file '%s' should exist in '%s'", Filename, suite.TestDir)

	// Set to read only.
	err = os.Chmod(file, 0400)
	if err != nil {
		panic(err)
	}
	err = WriteToFile()
	assert.Error(err, "Try to write to a file with read only permissions should return an error")

	// Use a path that not exists.
	Path = filepath.Join(suite.TestDir, "something/")
	path := filepath.Join(Path, Filename)
	err = WriteToFile()
	assert.Error(err, "The configs shouldn't be written to the file because the path does not exist")
	if !assert.NoFileExistsf(path, "The file shouldn't be created in the path '%s'",
		path) {
		removePath(Path)
	}
	Path = suite.TestDir // Restore the correct value
}

func (suite *ConfigTestSuite) TearDownTest() {
	removePath(suite.TestDir)
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func removePath(path string) {
	err := os.RemoveAll(path)
	if err != nil {
		panic(err)
	}
}
