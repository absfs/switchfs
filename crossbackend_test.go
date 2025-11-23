package switchfs

import (
	"testing"

	"github.com/absfs/memfs"
)

func TestCrossBackendMove(t *testing.T) {
	// Create two separate backends
	backend1, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("NewFS() error = %v", err)
	}
	backend2, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("NewFS() error = %v", err)
	}

	// Create switchfs with routes
	fs, err := New(
		WithRoute("/src", backend1, WithPriority(100)),
		WithRoute("/dst", backend2, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("cross-backend file move", func(t *testing.T) {
		// Create directory first
		if err := backend1.MkdirAll("/src", 0755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		// Create a test file in backend1
		content := []byte("test file content")
		file, err := backend1.Create("/src/testfile.txt")
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		file.Close()

		// Verify file exists in backend1
		if _, err := backend1.Stat("/src/testfile.txt"); err != nil {
			t.Fatalf("File not created in backend1: %v", err)
		}

		// Create destination directory
		if err := backend2.MkdirAll("/dst", 0755); err != nil {
			t.Fatalf("MkdirAll() dst error = %v", err)
		}

		// Move file from backend1 to backend2
		err = fs.Rename("/src/testfile.txt", "/dst/testfile.txt")
		if err != nil {
			t.Fatalf("Rename() error = %v", err)
		}

		// Verify file exists in backend2
		file2, err := backend2.Open("/dst/testfile.txt")
		if err != nil {
			t.Fatalf("File not found in backend2: %v", err)
		}
		defer file2.Close()

		// Verify content
		buf := make([]byte, len(content))
		n, err := file2.Read(buf)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if n != len(content) {
			t.Errorf("Read %d bytes, want %d", n, len(content))
		}
		if string(buf) != string(content) {
			t.Errorf("Content = %q, want %q", buf, content)
		}

		// Verify file removed from backend1
		if _, err := backend1.Stat("/src/testfile.txt"); err == nil {
			t.Errorf("File still exists in backend1 after move")
		}
	})

	t.Run("cross-backend directory move", func(t *testing.T) {
		// Create a directory structure in backend1
		if err := backend1.MkdirAll("/src/testdir/subdir", 0755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		// Create files in the directory
		files := map[string]string{
			"/src/testdir/file1.txt":        "content1",
			"/src/testdir/file2.txt":        "content2",
			"/src/testdir/subdir/file3.txt": "content3",
		}

		for path, content := range files {
			file, err := backend1.Create(path)
			if err != nil {
				t.Fatalf("Create(%s) error = %v", path, err)
			}
			file.Write([]byte(content))
			file.Close()
		}

		// Move directory from backend1 to backend2
		err := fs.Rename("/src/testdir", "/dst/testdir")
		if err != nil {
			t.Fatalf("Rename() directory error = %v", err)
		}

		// Verify all files exist in backend2 with correct content
		for srcPath, expectedContent := range files {
			// Convert src path to dst path
			dstPath := "/dst/testdir" + srcPath[len("/src/testdir"):]

			file, err := backend2.Open(dstPath)
			if err != nil {
				t.Errorf("File %s not found in backend2: %v", dstPath, err)
				continue
			}

			buf := make([]byte, 100)
			n, _ := file.Read(buf)
			file.Close()

			if string(buf[:n]) != expectedContent {
				t.Errorf("File %s content = %q, want %q", dstPath, buf[:n], expectedContent)
			}
		}

		// Verify directory removed from backend1
		if _, err := backend1.Stat("/src/testdir"); err == nil {
			t.Errorf("Directory still exists in backend1 after move")
		}
	})

	t.Run("same backend move", func(t *testing.T) {
		// Create a test file in backend1
		file, err := backend1.Create("/src/same-backend.txt")
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		file.Write([]byte("test"))
		file.Close()

		// Move within same backend should use native rename
		err = fs.Rename("/src/same-backend.txt", "/src/renamed.txt")
		if err != nil {
			t.Fatalf("Rename() same backend error = %v", err)
		}

		// Verify file exists at new location
		if _, err := backend1.Stat("/src/renamed.txt"); err != nil {
			t.Errorf("File not found at new location: %v", err)
		}

		// Verify file removed from old location
		if _, err := backend1.Stat("/src/same-backend.txt"); err == nil {
			t.Errorf("File still exists at old location")
		}
	})
}
