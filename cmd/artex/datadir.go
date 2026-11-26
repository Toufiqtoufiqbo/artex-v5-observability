package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// validateDataDir refuses a data directory that is the configuration base
// directory itself. -data is a browsable workspace; config.json and jwt.key
// live next to it. Pointing -data at the base dir would expose the signing key
// (and the config file) to the file manager and to any tool with workspace
// access. The default (<base>/data) is unaffected.
func validateDataDir(dataDir, baseDir string) error {
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}
	cleanData, cleanBase := filepath.Clean(absData), filepath.Clean(absBase)
	// EqualFold covers Windows/case-insensitive filesystems; the conservative
	// match can only refuse a startup that would be ambiguous anyway.
	if cleanData == cleanBase || strings.EqualFold(cleanData, cleanBase) {
		return errDataDirIsBaseDir
	}
	// Symlinks/aliases: ask the filesystem whether both names are one directory.
	if di, err := os.Stat(cleanData); err == nil {
		if bi, err := os.Stat(cleanBase); err == nil && os.SameFile(di, bi) {
			return errDataDirIsBaseDir
		}
	}
	return nil
}

var errDataDirIsBaseDir = errors.New("-data must not be the configuration base directory (choose a dedicated data directory, e.g. <base>/data)")
