package switchfs

import (
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/fstesting"
	"github.com/absfs/memfs"
)

func TestSwitchFS_WrapperSuite(t *testing.T) {
	baseFS, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create base filesystem: %v", err)
	}

	suite := &fstesting.WrapperSuite{
		Name:   "switchfs",
		BaseFS: baseFS,
		Factory: func(base absfs.FileSystem) (absfs.FileSystem, error) {
			return New(WithDefault(base))
		},
		TransformsData: false,
		TransformsMeta: false,
		ReadOnly:       false,
	}

	suite.Run(t)
}

func TestSwitchFS_Suite(t *testing.T) {
	backend, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create backend filesystem: %v", err)
	}

	fs, err := New(WithDefault(backend))
	if err != nil {
		t.Fatalf("failed to create switchfs: %v", err)
	}

	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			Symlinks:      false,
			HardLinks:     false,
			Permissions:   true,
			Timestamps:    true,
			CaseSensitive: true,
			AtomicRename:  true,
			SparseFiles:   false,
			LargeFiles:    false,
		},
	}

	suite.Run(t)
}

func TestSwitchFS_MultiBackend(t *testing.T) {
	hotBackend, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create hot backend: %v", err)
	}

	coldBackend, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create cold backend: %v", err)
	}

	fs, err := New(
		WithDefault(coldBackend),
		WithRoute("/hot", hotBackend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("failed to create switchfs: %v", err)
	}

	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			Symlinks:      false,
			HardLinks:     false,
			Permissions:   true,
			Timestamps:    true,
			CaseSensitive: true,
			AtomicRename:  true,
			SparseFiles:   false,
			LargeFiles:    false,
		},
	}

	suite.Run(t)
}
