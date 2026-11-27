package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reportingStore is a store in storeFolder keeping entryCapacity entries, and the failures it reports.
func reportingStore(storeFolder string, entryCapacity int) (*SchemaFolderStore, *[]string) {
	var reportedFailures []string
	folderStore := NewSchemaFolderStore(storeFolder, entryCapacity, func(failureMessage string) {
		reportedFailures = append(reportedFailures, failureMessage)
	})
	return folderStore, &reportedFailures
}

func ageEntry(testingContext *testing.T, folderStore *SchemaFolderStore, storeKey string, secondsAgo int) {
	agedAt := time.Now().Add(-time.Duration(secondsAgo) * time.Second)
	if timeError := os.Chtimes(folderStore.entryPath(storeKey), agedAt, agedAt); timeError != nil {
		testingContext.Fatal(timeError)
	}
}

func TestAnEntryIsKeptAsAFileAndReadBack(testingContext *testing.T) {
	scratchFolder := filepath.Join(testingContext.TempDir(), "mock-store")
	folderStore, reportedFailures := reportingStore(scratchFolder, 2)
	if _, isStored := folderStore.Read("mock-key"); isStored {
		testingContext.Fatal("an empty store reads an entry")
	}
	folderStore.Write("mock-key", `{"mock": 1}`)
	if storedText, isStored := folderStore.Read("mock-key"); !isStored || storedText != `{"mock": 1}` {
		testingContext.Fatalf("read %q", storedText)
	}
	if fileInfo, statError := os.Stat(filepath.Join(scratchFolder, "mock-key.json")); statError != nil ||
		!fileInfo.Mode().IsRegular() {
		testingContext.Fatal("the entry is no file")
	}
	if len(*reportedFailures) != 0 {
		testingContext.Fatalf("reported %v", *reportedFailures)
	}
}

func TestAWriteDropsTheLeastRecentlyUsedEntriesBeyondTheCapacity(testingContext *testing.T) {
	folderStore, reportedFailures := reportingStore(testingContext.TempDir(), 2)
	folderStore.Write("oldest", "1")
	folderStore.Write("used", "2")
	ageEntry(testingContext, folderStore, "oldest", 300)
	ageEntry(testingContext, folderStore, "used", 200)
	folderStore.Read("used")
	folderStore.Write("newest", "3")
	if _, isStored := folderStore.Read("oldest"); isStored {
		testingContext.Fatal("the least recently used entry was kept")
	}
	for storeKey, expectedText := range map[string]string{"used": "2", "newest": "3"} {
		if storedText, _ := folderStore.Read(storeKey); storedText != expectedText {
			testingContext.Fatalf("%s reads %q", storeKey, storedText)
		}
	}
	if len(*reportedFailures) != 0 {
		testingContext.Fatalf("reported %v", *reportedFailures)
	}
}

func TestAWriteThatFailsLeavesNoPartialEntryAndIsReported(testingContext *testing.T) {
	scratchFolder := testingContext.TempDir()
	blockingFile := filepath.Join(scratchFolder, "mock-file")
	if writeError := os.WriteFile(blockingFile, []byte("mock"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	storeFolder := filepath.Join(blockingFile, "mock-store")
	folderStore, reportedFailures := reportingStore(storeFolder, 1)
	folderStore.Write("mock-key", "1")
	if _, isStored := folderStore.Read("mock-key"); isStored {
		testingContext.Fatal("a store under a file kept an entry")
	}
	savePrefix := "failed to save schema store entry `" + filepath.Join(storeFolder, "mock-key.json") + "`: "
	if len(*reportedFailures) != 1 || !strings.HasPrefix((*reportedFailures)[0], savePrefix) {
		testingContext.Fatalf("reported %v", *reportedFailures)
	}
	defer func() {
		if recover() == nil {
			testingContext.Fatal("a store of no capacity was made")
		}
	}()
	NewSchemaFolderStore(scratchFolder, 0, func(string) {})
}
