package models

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions"
)

func TestCompressFilesOfDir(t *testing.T) {
	// Logs
	writer := io.Writer(os.Stdout)
	log.SetOutput(writer)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	baseDir := filepath.Join(dir, "/test/")
	filesDir := filepath.Join(baseDir, "/targetFiles/")
	outputDir := filepath.Join(baseDir, "/output/")

	defer os.RemoveAll(baseDir)
	defer os.RemoveAll(filepath.Join(dir, "/data/"))

	var testInfo = []struct {
		files    []string
		fakeData data.UserAction
	}{

		{
			files: []string{"afile.txt", "other_file.odt", "fake_image.png", "test.svg"},
			fakeData: data.UserAction{
				ID: "A1",
				Args: []data.UserArg{
					data.UserArg{
						ID:      "A2-1",
						Content: filesDir,
					},
					data.UserArg{
						ID:      "A2-2",
						Content: outputDir,
					},
				},
				Timestamp: "",
				Chained:   false,
				Order:     0,
			},
		},

		{
			files: []string{"afile.exe", "other_file.jpg", "file.iso", "test.o"},
			fakeData: data.UserAction{
				ID: "A1",
				Args: []data.UserArg{
					data.UserArg{
						ID:      "A2-1",
						Content: filesDir,
					},
					data.UserArg{
						ID:      "A2-2",
						Content: outputDir,
					},
				},
				Timestamp: "",
				Chained:   false,
				Order:     0,
			},
		},
	}

	fmt.Println("Making dirs for testing...")
	// Create dirs
	err = os.Mkdir(baseDir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(filesDir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(outputDir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("Done, dirs created")

	fmt.Println("Doing tests...")
	for index, info := range testInfo {
		fmt.Printf("Starting test %d...\n", index+1)
		for _, file := range info.files {
			fullpath := filepath.Join(filesDir, file)
			fmt.Printf("Making file '%s'\n", fullpath)
			f, err := os.Create(fullpath)
			if err != nil {
				t.Error(err)
			}
			f.Close()
			fmt.Println("Done")

			fmt.Println("Generating a random size on it...")
			size := rand.Int63n(10000)
			err = os.Truncate(fullpath, size)
			if err != nil {
				t.Error(err)
			}
			fmt.Println("Done, file size (bytes):", size)

		}

		fmt.Println("Running CompressFilesOfDir.Run...")
		result, _, err := CompressFilesOfDir.Run(&actions.ChainedResult{}, &info.fakeData, "TEST")
		if err != nil {
			t.Error(err)
			continue
		}

		if !result {
			t.Error("Result false, action no executed")
			continue
		}
		fmt.Println("Done!, result:", result)
	}
}
