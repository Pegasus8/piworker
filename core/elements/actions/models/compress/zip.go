package compress

import (
	"archive/zip"
	"io/ioutil"
	"os"
	"path/filepath"
)

func zipWriter(targetPath, outputPath string) error {
	target := filepath.Clean(targetPath)
	output := filepath.Clean(outputPath)

	outputFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	w := zip.NewWriter(outputFile)

	err = addFiles(w, target, "")
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return nil
}

func addFiles(w *zip.Writer, basePath, baseInZip string) error {
	files, err := ioutil.ReadDir(basePath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !file.IsDir() {
			content, err := ioutil.ReadFile(filepath.Join(basePath, file.Name()))
			if err != nil {
				return err
			}

			f, err := w.Create(baseInZip + file.Name())
			if err != nil {
				return err
			}

			_, err = f.Write(content)
			if err != nil {
				return err
			}
		} else {
			// If it's a directory, execute the function recursively.
			newBase := filepath.Join(basePath, file.Name())
			err := addFiles(w, newBase, baseInZip+file.Name()+"/")
			if err != nil {
				return err
			}
		}
	}

	return nil
}
