package configs

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ReadConfigTestSuite struct {
	TestDir string
	suite.Suite
}

func (suite *ReadConfigTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir

	CurrentConfigs = &DefaultConfigs

	err := os.Mkdir(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	// Initialize the file to do tests.
	err = WriteToFile()
	if err != nil {
		panic(err)
	}

}

func (suite *ReadConfigTestSuite) TestReadFromFile() {
	assert := assert2.New(suite.T())
	file := filepath.Join(suite.TestDir, Filename)

	// --- Test 1 ---
	err := ReadFromFile()
	assert.NoError(err, "The file of configs should be read correctly")
	// --- End of Test 1 ---

	// --- Test 2 ---
	// Change permissions to write only, which should cause an error when trying to read them.
	err = os.Chmod(file, 0200)
	if err != nil {
		panic(err)
	}

	err = ReadFromFile()
	assert.Error(err, "Trying to read a file with write only permissions should return an error")
	assert.EqualError(err, "open test/configs.json: "+os.ErrPermission.Error(), "The error returned is not which should be")
	// --- End of Test 2 ---

	// --- Test 3 ---
	// Restore permissions.
	err = os.Chmod(file, 0644)
	if err != nil {
		panic(err)
	}

	// Get the size of the file to truncate part of it.
	fstat, err := os.Stat(file)
	if err != nil {
		panic(err)
	}

	// Truncate part of the content to make it corrupt.
	var s int64
	for {
		// Let's make sure that we don't get a zero.
		s = rand.Int63n(fstat.Size() - 10)
		if s != 0 {
			break
		}
	}

	err = os.Truncate(file, s)
	if err != nil {
		panic(err)
	}

	err = ReadFromFile()
	assert.Error(err, "Trying to read a corrupted config file should return an error")
	assert.EqualError(err, ErrConfigFileCorrupted.Error(), "The error returned is not which should be")
	// --- End of Test 3 ---

	// --- Test 4 ---
	// Remove the file.
	err = os.Remove(file)
	if err != nil {
		panic(err)
	}

	err = ReadFromFile()
	assert.Error(err, "Trying to read a non-existent config file should return an error")
	assert.EqualError(err, ErrNoConfigFileDetected.Error(), "The error returned is not which should be")
	// --- End of Test 4 ---
}

func (suite *ReadConfigTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestRFFSuite(t *testing.T) {
	suite.Run(t, new(ReadConfigTestSuite))
}
