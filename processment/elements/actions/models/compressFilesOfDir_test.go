package models 

import (
	"testing"
	"os"
	"path/filepath"
	"math/rand"
	"io"
	"fmt"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/utilities/log"
)

func TestCompressFilesOfDir(t *testing.T) {
	// Logs
	writer := io.Writer(os.Stdout)
	log.Init(writer,writer,writer,writer,writer)
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
		files []string
		fakeData []data.UserArg
	} {

		{[]string{"afile.txt", "other_file.odt", "fake_image.png", "test.svg"}, 
		[]data.UserArg {
			data.UserArg {
				ID: "A2-1",
				Content: filesDir,
			},
			data.UserArg {
				ID: "A2-2",
				Content: outputDir,
			},
		}},

		{[]string{"afile.exe", "other_file.jpg", "file.iso", "test.o"},
		[]data.UserArg {
			data.UserArg {
				ID: "A2-1",
				Content: filesDir,
			},
			data.UserArg {
				ID: "A2-2",
				Content: outputDir,
			},
		}},
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
		result, err := CompressFilesOfDir.Run(&info.fakeData)
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