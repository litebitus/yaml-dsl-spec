package yamldslspec

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

const (
	cachedSchemaText      = `{"type": "object", "properties": {"name": {"type": "string"}}}`
	otherCachedSchemaText = `{"type": "object", "required": ["name"]}`
)

// mockSchemaStore is a store in memory, counting what it is asked to read and write.
type mockSchemaStore struct {
	storeLock     sync.Mutex
	storedEntries map[string]string
	readCount     int
	writeCount    int
}

func (store *mockSchemaStore) Read(storeKey string) (string, bool) {
	store.storeLock.Lock()
	defer store.storeLock.Unlock()
	store.readCount++
	storedText, isStored := store.storedEntries[storeKey]
	return storedText, isStored
}

func (store *mockSchemaStore) Write(storeKey, preparedText string) {
	store.storeLock.Lock()
	defer store.storeLock.Unlock()
	store.writeCount++
	store.storedEntries[storeKey] = preparedText
}

func TestASchemaIsPreparedOnceAndReadFromTheStoreByALaterCache(t *testing.T) {
	store := &mockSchemaStore{storedEntries: map[string]string{}}
	if _, loadError := NewSchemaCache(WithStore(store)).Load("mock.schema.json", cachedSchemaText); loadError != nil {
		t.Fatal(loadError)
	}
	if store.writeCount != 1 {
		t.Fatalf("writes = %d, want 1", store.writeCount)
	}
	for _, storedText := range store.storedEntries {
		for _, preparedPart := range []string{`"draft":"draft-07"`, `"schema":{"type":"object"`, `"marked":{`} {
			if !strings.Contains(storedText, preparedPart) {
				t.Fatalf("stored %s, want it to hold %s", storedText, preparedPart)
			}
		}
	}
	if _, loadError := NewSchemaCache(WithStore(store)).Load("mock.schema.json", cachedSchemaText); loadError != nil {
		t.Fatal(loadError)
	}
	if store.writeCount != 1 || store.readCount != 2 {
		t.Fatalf("writes = %d, reads = %d, want 1 and 2", store.writeCount, store.readCount)
	}
}

func TestAStoredEntryThatDoesNotReadIsPreparedAgainFromTheText(t *testing.T) {
	store := &mockSchemaStore{storedEntries: map[string]string{}}
	store.Write(schemaStoreKey("mock.schema.json", cachedSchemaText), "not prepared")
	if _, loadError := NewSchemaCache(WithStore(store)).Load("mock.schema.json", cachedSchemaText); loadError != nil {
		t.Fatal(loadError)
	}
	if store.writeCount != 2 {
		t.Fatalf("writes = %d, want 2", store.writeCount)
	}
}

func TestTheLeastRecentlyAskedForSchemaIsDroppedPastTheCapacity(t *testing.T) {
	schemaCache := NewSchemaCache(WithCapacity(2))
	firstSchema, _ := schemaCache.Load("first.json", cachedSchemaText)
	secondSchema, _ := schemaCache.Load("second.json", cachedSchemaText)
	schemaCache.Load("first.json", cachedSchemaText)
	schemaCache.Load("third.json", otherCachedSchemaText)
	if reloaded, _ := schemaCache.Load("first.json", cachedSchemaText); reloaded != firstSchema {
		t.Fatal("the schema asked for most recently was dropped")
	}
	if reloaded, _ := schemaCache.Load("second.json", cachedSchemaText); reloaded == secondSchema {
		t.Fatal("the schema asked for least recently was held past the capacity")
	}
}

func TestACapacityBelowOneIsRefused(t *testing.T) {
	defer func() {
		if refusal := recover(); refusal == nil || !strings.Contains(refusal.(string), "whole number from 1, not 0") {
			t.Fatalf("recovered %v, want the capacity refused", refusal)
		}
	}()
	WithCapacity(0)
}

func TestTheStoreKeyNamesTheSchemaItsTextAndTheModuleVersion(t *testing.T) {
	storeKey := schemaStoreKey("mock.schema.json", cachedSchemaText)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(storeKey) {
		t.Fatalf("key %s is no sha256 in hex", storeKey)
	}
	if storeKey != schemaStoreKey("mock.schema.json", cachedSchemaText) ||
		storeKey == schemaStoreKey("other.schema.json", cachedSchemaText) ||
		storeKey == schemaStoreKey("mock.schema.json", otherCachedSchemaText) {
		t.Fatal("the key does not follow the name and the text")
	}
}
