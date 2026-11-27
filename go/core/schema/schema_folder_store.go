package schema

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

const folderEntryExtension = ".json"

// SchemaFolderStore is a schema store keeping each entry as a file in a folder, the least recently used dropped
// past its capacity.
type SchemaFolderStore struct {
	storeFolder   string
	entryCapacity int
	reportFailure func(failureMessage string)
}

// NewSchemaFolderStore is a store keeping at most entryCapacity entries in storeFolder, each entry it fails to save
// or drop reported to reportFailure; it panics below one entry.
func NewSchemaFolderStore(
	storeFolder string, entryCapacity int, reportFailure func(failureMessage string),
) *SchemaFolderStore {
	if entryCapacity < 1 {
		panic(fmt.Sprintf("a schema folder store's capacity is a whole number from 1, not %d", entryCapacity))
	}
	return &SchemaFolderStore{storeFolder: storeFolder, entryCapacity: entryCapacity, reportFailure: reportFailure}
}

func (folderStore *SchemaFolderStore) entryPath(storeKey string) string {
	return filepath.Join(folderStore.storeFolder, storeKey+folderEntryExtension)
}

// report reports a failure on the file or folder at failedPath.
func (folderStore *SchemaFolderStore) report(failedAction, failedPath string, failureError error) {
	folderStore.reportFailure(
		"failed to " + failedAction + " " + core.CodeSpan(failedPath) + ": " + failureError.Error())
}

// Read is the entry written under storeKey, and whether the folder holds it; reading it marks it used.
func (folderStore *SchemaFolderStore) Read(storeKey string) (string, bool) {
	entryPath := folderStore.entryPath(storeKey)
	entryBytes, readError := os.ReadFile(entryPath)
	if readError != nil {
		return "", false
	}
	markUsed(entryPath)
	return string(entryBytes), true
}

// Write writes the entry beside its place and moves it there, so a reader never reads half an entry, then drops
// the least recently used entries beyond the capacity.
func (folderStore *SchemaFolderStore) Write(storeKey, preparedText string) {
	entryPath := folderStore.entryPath(storeKey)
	partialPath := strings.TrimSuffix(entryPath, folderEntryExtension) + fmt.Sprintf(".%d.partial", os.Getpid())
	writeError := os.MkdirAll(folderStore.storeFolder, 0o777)
	if writeError == nil {
		writeError = os.WriteFile(partialPath, []byte(preparedText), 0o666)
	}
	if writeError == nil {
		writeError = os.Rename(partialPath, entryPath)
	}
	if writeError != nil {
		_ = os.Remove(partialPath)
		folderStore.report("save schema store entry", entryPath, writeError)
		return
	}
	folderStore.dropLeastRecentlyUsed()
}

// dropLeastRecentlyUsed drops the least recently used entries beyond the capacity.
func (folderStore *SchemaFolderStore) dropLeastRecentlyUsed() {
	folderEntries, listError := os.ReadDir(folderStore.storeFolder)
	if listError != nil {
		folderStore.report("list schema store folder", folderStore.storeFolder, listError)
		return
	}
	type usedEntry struct {
		entryPath string
		usedAt    time.Time
	}
	var usedEntries []usedEntry
	for _, folderEntry := range folderEntries {
		entryName := folderEntry.Name()
		if len(entryName) <= len(folderEntryExtension) || !strings.HasSuffix(entryName, folderEntryExtension) {
			continue
		}
		entryPath := filepath.Join(folderStore.storeFolder, entryName)
		if entryInfo, statError := os.Stat(entryPath); statError == nil {
			usedEntries = append(usedEntries, usedEntry{entryPath: entryPath, usedAt: entryInfo.ModTime()})
		}
	}
	sort.SliceStable(usedEntries, func(firstIndex, secondIndex int) bool {
		return usedEntries[firstIndex].usedAt.After(usedEntries[secondIndex].usedAt)
	})
	for _, droppedEntry := range usedEntries[min(folderStore.entryCapacity, len(usedEntries)):] {
		removeError := os.Remove(droppedEntry.entryPath)
		if removeError != nil && !errors.Is(removeError, fs.ErrNotExist) {
			folderStore.report("drop schema store entry", droppedEntry.entryPath, removeError)
		}
	}
}

// markUsed marks the entry at entryPath used now, its modified time being when it was last used.
func markUsed(entryPath string) {
	usedNow := time.Now()
	_ = os.Chtimes(entryPath, usedNow, usedNow)
}
