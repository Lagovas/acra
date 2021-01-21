package tests

import (
	"os"
	"path/filepath"
	"testing"
)

// GetSourceRootDirectory expects that tests started from root of source code and accessible tests/ssl folder, otherwise try to walk
// through parents folders and check that it's root of source until find or call t.Fatal
// It useful because `go test ./...` change working directory recursively to each packages when it compile tests
func GetSourceRootDirectory(t testing.TB) string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	requiredFiles := []string{
		"tests/ssl/acra-writer/acra-writer.crt",
		"tests/ssl/acra-writer/acra-writer.key",
	}
	// find any path starting from current working directory and walk to root of FS until find path when accessible first
	// required file
	for {
		testPath := filepath.Join(workingDirectory, requiredFiles[0])
		info, err := os.Lstat(testPath)
		if err != nil {
			workingDirectory = filepath.Dir(workingDirectory)
			if workingDirectory == "/" {
				t.Fatal("Can't find root of sources as working directory, please run tests from root of sources")
			}
			continue
		}
		if info.IsDir() {
			t.Fatalf("'%s' is directory but expects file\n", testPath)
		}
		break
	}

	for _, path := range requiredFiles {
		_, err := os.Lstat(filepath.Join(workingDirectory, path))
		if err != nil {
			t.Fatalf("Not found file %s, took '%s' error\n", path, err)
		}
	}

	return workingDirectory
}